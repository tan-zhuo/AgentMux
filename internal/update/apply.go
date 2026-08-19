package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// oldSuffix marks the executable (or app bundle) that was just replaced. It
// cannot be deleted while it is still running, so it is renamed aside and
// swept up on the next start.
const oldSuffix = ".old"

// Apply swaps the running installation for the build inside archivePath and
// returns the path to start the new version from. The old executable is left
// beside the new one with a ".old" suffix — Windows cannot delete a running
// program, and on every platform it doubles as the rollback if the swap dies
// halfway.
func Apply(archivePath, stagingDir string) (restartPath string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if runtime.GOOS == "darwin" {
		return applyDarwin(archivePath, stagingDir, exe)
	}
	return applyBinary(archivePath, stagingDir, exe)
}

// applyBinary handles Linux and Windows, where the unit of installation is a
// single executable file.
func applyBinary(archivePath, stagingDir, exe string) (string, error) {
	binName := "agentmux"
	if runtime.GOOS == "windows" {
		binName = "agentmux.exe"
	}
	newBin := filepath.Join(stagingDir, "new-"+binName)

	var err error
	if strings.HasSuffix(archivePath, ".tar.gz") {
		err = extractTarGzFile(archivePath, "/"+binName, newBin)
	} else {
		err = extractZipFile(archivePath, "/"+binName, newBin)
	}
	if err != nil {
		return "", fmt.Errorf("could not unpack the new build: %w", err)
	}
	if err := os.Chmod(newBin, 0o755); err != nil {
		return "", err
	}

	old := exe + oldSuffix
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return "", fmt.Errorf("could not move the current program aside — is %s writable? %w",
			filepath.Dir(exe), err)
	}
	if err := moveFile(newBin, exe); err != nil {
		// The new build did not land; put the old one back so the install
		// still works.
		if rbErr := os.Rename(old, exe); rbErr != nil {
			return "", fmt.Errorf("update failed and rollback failed too — reinstall from the release page: %v (rollback: %v)", err, rbErr)
		}
		return "", fmt.Errorf("could not install the new build: %w", err)
	}
	return exe, nil
}

// applyDarwin replaces the whole .app bundle. ditto does the unpacking: it is
// on every Mac and is the only tool guaranteed to reproduce exactly what the
// release job packed with it.
func applyDarwin(archivePath, stagingDir, exe string) (string, error) {
	bundle := bundleRoot(exe)
	if bundle == "" {
		return "", fmt.Errorf("not running from an .app bundle — replace this build the way it was installed")
	}

	unpackDir := filepath.Join(stagingDir, "unpacked")
	if err := os.MkdirAll(unpackDir, 0o755); err != nil {
		return "", err
	}
	if out, err := exec.Command("ditto", "-xk", archivePath, unpackDir).CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not unpack the new build: %v: %s", err, strings.TrimSpace(string(out)))
	}
	newApp := filepath.Join(unpackDir, filepath.Base(bundle))
	if _, err := os.Stat(newApp); err != nil {
		return "", fmt.Errorf("the archive does not contain %s", filepath.Base(bundle))
	}

	old := bundle + oldSuffix
	os.RemoveAll(old)
	if err := os.Rename(bundle, old); err != nil {
		return "", fmt.Errorf("could not move the current app aside — is %s writable? %w",
			filepath.Dir(bundle), err)
	}
	if err := os.Rename(newApp, bundle); err != nil {
		// Staging may sit on a different volume than /Applications; ditto
		// copies across that boundary.
		if out, cpErr := exec.Command("ditto", newApp, bundle).CombinedOutput(); cpErr != nil {
			if rbErr := os.Rename(old, bundle); rbErr != nil {
				return "", fmt.Errorf("update failed and rollback failed too — reinstall from the release page: %v (rollback: %v)", cpErr, rbErr)
			}
			return "", fmt.Errorf("could not install the new app: %v: %s", cpErr, strings.TrimSpace(string(out)))
		}
	}
	// The file came off the network; make sure Gatekeeper's quarantine flag,
	// if anything set it, does not greet the next launch.
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", bundle).Run()
	return bundle, nil
}

// bundleRoot walks up from the executable to the .app directory, or returns
// empty when there is none.
//
// The walk stops where the parent stops changing, which is the only portable
// way to recognise a filesystem root: comparing against "/" holds on macOS and
// spins forever on Windows, where filepath.Dir(`C:\`) is `C:\` — and this
// runs on every start, from CleanupOld, on every platform.
func bundleRoot(exe string) string {
	for dir := filepath.Dir(exe); dir != "."; {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// Relaunch starts the freshly installed version. The caller quits right after,
// so the child is deliberately detached from this process.
func Relaunch(restartPath string) error {
	if runtime.GOOS == "darwin" {
		// Through LaunchServices, so the new instance is a real app launch
		// rather than a child of the one that is about to exit.
		return exec.Command("open", "-n", restartPath).Start()
	}
	cmd := exec.Command(restartPath)
	detach(cmd)
	return cmd.Start()
}

// CleanupOld sweeps away what a previous update left behind: the renamed old
// executable (or bundle) and the staging directory. Called on every start;
// quiet when there is nothing to do.
func CleanupOld(stagingDir string) {
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		os.Remove(exe + oldSuffix)
		if bundle := bundleRoot(exe); bundle != "" {
			os.RemoveAll(bundle + oldSuffix)
		}
	}
	if stagingDir != "" {
		os.RemoveAll(stagingDir)
	}
}

// moveFile renames, and falls back to copying when source and destination sit
// on different filesystems.
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(dst)
		return err
	}
	os.Remove(src)
	return nil
}

// extractTarGzFile pulls the one entry whose path ends in wantSuffix out of a
// .tar.gz archive.
func extractTarGzFile(archive, wantSuffix, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("no %s inside the archive", wantSuffix)
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag != tar.TypeReg || !strings.HasSuffix("/"+hdr.Name, wantSuffix) {
			continue
		}
		return writeFile(dest, tr)
	}
}

// extractZipFile does the same for a .zip archive.
func extractZipFile(archive, wantSuffix, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasSuffix("/"+f.Name, wantSuffix) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeFile(dest, rc)
	}
	return fmt.Errorf("no %s inside the archive", wantSuffix)
}

func writeFile(dest string, r io.Reader) error {
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
