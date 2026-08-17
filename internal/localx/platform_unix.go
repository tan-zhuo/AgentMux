//go:build !windows

package localx

import (
	"errors"
	"os"
	"os/exec"
)

// shellCommand runs a POSIX command line on this machine. A login shell is used
// so the command sees the PATH the user's own terminal would: agents installed
// by nvm, asdf, homebrew or a dotfile live there and nowhere else.
func shellCommand(cmd string) (string, []string) {
	return shellPath(), []string{"-lc", cmd}
}

// terminalCommand starts an interactive terminal. An empty line means the user's
// own login shell, which is what a plain "open a shell here" asks for.
func terminalCommand(line string) (string, []string) {
	if line == "" {
		shell := os.Getenv("SHELL")
		if shell == "" {
			shell = shellPath()
		}
		return shell, []string{"-l"}
	}
	return shellPath(), []string{"-lc", line}
}

func shellPath() string {
	for _, p := range []string{"/bin/sh", "/usr/bin/sh"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath("sh"); err == nil {
		return p
	}
	return "/bin/sh"
}

// platformAvailable reports whether this machine can be managed as a host.
//
// tmux is deliberately not required here. A local host without it is still worth
// having — shells, files, metrics — and the install panel can put tmux on it the
// same way it does for a remote host; what would not work is agents, and the
// probe says so through HasTmux.
func platformAvailable() error {
	if _, err := os.Stat(shellPath()); err != nil {
		return errors.New("this computer has no POSIX shell at /bin/sh, which everything AgentMux runs needs")
	}
	return nil
}

// hostPath maps a path as the local shell sees it to one this process can open.
// On Unix they are the same path, which is the whole reason this is a seam rather
// than a conversion.
func hostPath(p string) (string, error) { return p, nil }

// shellPathOf is the inverse of hostPath: the path as the shell sees it.
func shellPathOf(p string) string { return p }
