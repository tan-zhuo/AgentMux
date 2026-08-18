//go:build windows

package localx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// On Windows the POSIX local host is WSL.
//
// Work survives the window closing because it lives in a tmux session, and tmux
// exists on Windows only inside WSL. So this host kind means the default WSL
// distribution — its shell, its tmux, its filesystem — and the paths above this
// file stay POSIX throughout. The machine's native Windows side is its own host
// kind, served by the Native* types and the natmux session daemon.
const (
	wslExe = "wsl.exe"
	// The share Windows exposes the WSL filesystem on. The older form is tried as
	// a fallback for Windows 10 builds that predate it.
	wslShare    = `\\wsl.localhost\`
	wslShareOld = `\\wsl$\`
)

func shellCommand(cmd string) (string, []string) {
	return wslExe, []string{"-e", "sh", "-lc", cmd}
}

func terminalCommand(line string) (string, []string) {
	if line == "" {
		// The login shell of the WSL account, not sh: it is where the user's PATH
		// and their agent CLIs actually are.
		return wslExe, []string{"-e", "sh", "-lc", `exec "${SHELL:-/bin/sh}" -l`}
	}
	return wslExe, []string{"-e", "sh", "-lc", line}
}

func platformAvailable() error {
	if _, err := exec.LookPath(wslExe); err != nil {
		return errors.New(
			"managing this computer needs WSL, because tmux — which is what keeps an agent " +
				"running after the window closes — only exists there on Windows. Install it with " +
				"`wsl --install`, then try again")
	}
	out, err := exec.Command(wslExe, "-e", "sh", "-lc", "printf ok").Output()
	if err != nil || !strings.Contains(string(out), "ok") {
		return errors.New(
			"WSL is installed but has no working distribution yet. Run `wsl --install -d Ubuntu`, " +
				"finish its first-run setup, then try again")
	}
	return nil
}

var (
	distroOnce sync.Once
	distroName string
	distroErr  error
)

// distro is the name of the default WSL distribution, asked from inside it so the
// answer is plain UTF-8 — `wsl -l` reports in UTF-16, which is a parsing problem
// nobody needs.
func distro() (string, error) {
	distroOnce.Do(func() {
		out, err := exec.Command(wslExe, "-e", "sh", "-lc", `printf %s "$WSL_DISTRO_NAME"`).Output()
		name := strings.TrimSpace(string(out))
		if err != nil || name == "" {
			distroErr = errors.New("could not tell which WSL distribution is the default one")
			return
		}
		distroName = name
	})
	return distroName, distroErr
}

// hostPath maps a POSIX path inside WSL to the UNC path Windows reaches it by, so
// the file browser and editor can use ordinary file operations on it.
func hostPath(p string) (string, error) {
	name, err := distro()
	if err != nil {
		return "", err
	}
	rel := filepath.FromSlash(strings.TrimPrefix(p, "/"))
	unc := filepath.Join(wslShare+name, rel)
	if _, err := os.Stat(filepath.Join(wslShare + name)); err != nil {
		// Older Windows 10 exposes the same filesystem under a different share.
		if _, err2 := os.Stat(filepath.Join(wslShareOld + name)); err2 == nil {
			return filepath.Join(wslShareOld+name, rel), nil
		}
		return "", fmt.Errorf("cannot reach the WSL filesystem at %s%s", wslShare, name)
	}
	return unc, nil
}

// shellPathOf converts a Windows path back to the POSIX path the shell inside WSL
// would use, which is what every path AgentMux stores looks like.
func shellPathOf(p string) string {
	s := filepath.ToSlash(p)
	for _, share := range []string{filepath.ToSlash(wslShare), filepath.ToSlash(wslShareOld)} {
		if strings.HasPrefix(s, share) {
			rest := strings.TrimPrefix(s, share)
			if i := strings.Index(rest, "/"); i >= 0 {
				return rest[i:]
			}
			return "/"
		}
	}
	return s
}
