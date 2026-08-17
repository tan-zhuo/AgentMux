package localx

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"agentmux/internal/sftpx"
)

// editLimit matches the SFTP editor's ceiling. Past it the editor stops being
// useful and starts being a way to hang the UI on a four-gigabyte log.
const editLimit = 4 << 20

// Files reads and writes this machine's filesystem, returning the same shapes the
// SFTP client does so the browser and the editor cannot tell which host they are
// looking at.
//
// Paths are POSIX everywhere above this type, because that is what the shell, the
// tmux sessions and the workspace records use. On Windows they are paths inside
// WSL, and hostPath is what turns one into something this process can open.
type Files struct{ run *Runner }

// NewFiles builds the local file layer.
func NewFiles(run *Runner) *Files { return &Files{run: run} }

// Home is the home directory of the local shell.
func (f *Files) Home(_ string) (string, error) { return f.run.Home() }

// List reads a directory. An empty path opens the home directory.
func (f *Files) List(serverID, dir string) (sftpx.Listing, error) {
	if strings.TrimSpace(dir) == "" || dir == "~" {
		home, err := f.run.Home()
		if err != nil {
			return sftpx.Listing{}, err
		}
		dir = home
	}
	dir = path.Clean(dir)

	local, err := hostPath(dir)
	if err != nil {
		return sftpx.Listing{}, err
	}
	infos, err := os.ReadDir(local)
	if err != nil {
		return sftpx.Listing{}, fmt.Errorf("%s: %w", dir, err)
	}

	entries := make([]sftpx.Entry, 0, len(infos))
	for _, de := range infos {
		full := path.Join(dir, de.Name())
		fi, err := de.Info()
		if err != nil {
			// A file that vanished between the listing and the stat is simply not
			// in the listing.
			continue
		}
		e := sftpx.Entry{
			Name:    de.Name(),
			Path:    full,
			IsDir:   fi.IsDir(),
			Size:    fi.Size(),
			Mode:    fi.Mode().String(),
			ModTime: fi.ModTime().Unix(),
			IsLink:  fi.Mode()&os.ModeSymlink != 0,
		}
		if e.IsLink {
			if target, lerr := os.Readlink(filepath.Join(local, de.Name())); lerr == nil {
				e.Target = shellPathOf(target)
				// Stat follows the link; a broken one simply stays a file.
				if st, serr := os.Stat(filepath.Join(local, de.Name())); serr == nil {
					e.TargetIsDir = st.IsDir()
				}
			}
		}
		entries = append(entries, e)
	}

	// Directories first, then case-insensitive by name — the order people expect
	// from a file manager, and the order the SFTP listing uses.
	sort.SliceStable(entries, func(i, j int) bool {
		di := entries[i].IsDir || entries[i].TargetIsDir
		dj := entries[j].IsDir || entries[j].TargetIsDir
		if di != dj {
			return di
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	parent := path.Dir(dir)
	if parent == dir {
		parent = ""
	}
	return sftpx.Listing{ServerID: serverID, Path: dir, Parent: parent, Entries: entries}, nil
}

// Mkdir creates a directory and any missing parents.
func (f *Files) Mkdir(_ string, dir string) error {
	local, err := hostPath(dir)
	if err != nil {
		return err
	}
	return os.MkdirAll(local, 0o755)
}

// Rename moves a file or directory.
func (f *Files) Rename(_ string, from, to string) error {
	src, err := hostPath(from)
	if err != nil {
		return err
	}
	dst, err := hostPath(to)
	if err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// Remove deletes a path. A directory needs recursive set explicitly, so a
// mis-click cannot take a tree with it.
func (f *Files) Remove(_ string, target string, recursive bool) error {
	local, err := hostPath(target)
	if err != nil {
		return err
	}
	if recursive {
		return os.RemoveAll(local)
	}
	return os.Remove(local)
}

// ReadFile loads a text file for the editor, refusing the same things the SFTP
// reader refuses: directories, oversized files and binaries.
func (f *Files) ReadFile(_ string, target string) (sftpx.FileContent, error) {
	local, err := hostPath(target)
	if err != nil {
		return sftpx.FileContent{}, err
	}
	st, err := os.Stat(local)
	if err != nil {
		return sftpx.FileContent{}, fmt.Errorf("%s: %w", target, err)
	}
	if st.IsDir() {
		return sftpx.FileContent{}, fmt.Errorf("%s is a directory", target)
	}
	if st.Size() > editLimit {
		return sftpx.FileContent{}, fmt.Errorf(
			"%s is %s; the editor opens files up to 4 MiB. Use the terminal for anything larger",
			path.Base(target), humanSize(st.Size()))
	}

	raw, err := os.ReadFile(local)
	if err != nil {
		return sftpx.FileContent{}, err
	}
	// A NUL byte in the first chunk is the standard "this is not text" signal, and
	// opening a binary in a text editor corrupts it the moment it is saved.
	probe := raw
	if len(probe) > 8000 {
		probe = probe[:8000]
	}
	if bytes.IndexByte(probe, 0) >= 0 {
		return sftpx.FileContent{}, fmt.Errorf(
			"%s looks like a binary file, so it is not editable here", path.Base(target))
	}

	crlf := bytes.Contains(raw, []byte("\r\n"))
	text := string(raw)
	if crlf {
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}
	return sftpx.FileContent{
		Path:    target,
		Content: text,
		Size:    st.Size(),
		ModTime: st.ModTime().Unix(),
		Mode:    st.Mode().String(),
		CRLF:    crlf,
	}, nil
}

// WriteFile saves an edited file through a sibling temp file, keeping the
// modification-time guard that stops a save from overwriting a change an agent
// made in the same directory.
func (f *Files) WriteFile(
	_ string, target, content string, expectedModTime int64, crlf bool,
) (sftpx.FileContent, error) {
	local, err := hostPath(target)
	if err != nil {
		return sftpx.FileContent{}, err
	}
	if expectedModTime > 0 {
		if st, serr := os.Stat(local); serr == nil && st.ModTime().Unix() > expectedModTime {
			return sftpx.FileContent{}, fmt.Errorf(
				"%s changed on disk since you opened it. Reload before saving, or your edits will overwrite that change",
				path.Base(target))
		}
	}

	body := content
	if crlf {
		body = strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")
	}

	mode := os.FileMode(0o644)
	if st, serr := os.Stat(local); serr == nil {
		// Carry the original permissions over, or a saved script loses its
		// executable bit.
		mode = st.Mode().Perm()
	}

	tmp := local + ".agentmux-tmp"
	if err := os.WriteFile(tmp, []byte(body), mode); err != nil {
		return sftpx.FileContent{}, err
	}
	if err := os.Rename(tmp, local); err != nil {
		_ = os.Remove(tmp)
		return sftpx.FileContent{}, err
	}

	// Content is deliberately not echoed back: the caller just sent it. What it
	// needs is the new modification time, the baseline for the next outside change.
	st, err := os.Stat(local)
	if err != nil {
		return sftpx.FileContent{Path: target, CRLF: crlf}, nil //nolint:nilerr // the write succeeded
	}
	return sftpx.FileContent{
		Path:    target,
		Size:    st.Size(),
		ModTime: st.ModTime().Unix(),
		Mode:    st.Mode().String(),
		CRLF:    crlf,
	}, nil
}

// ErrNotTransferable is why a local host has no uploads and no downloads: the
// file is already on this machine, and copying it to itself is a file manager's
// job rather than this application's.
var ErrNotTransferable = errors.New(
	"this host is this computer, so there is nothing to transfer — the file is already here")

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	for _, u := range units {
		v /= unit
		if v < unit {
			return fmt.Sprintf("%.1f %s", v, u)
		}
	}
	return fmt.Sprintf("%.1f PiB", v/unit)
}
