//go:build !windows

package natmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// natmux only fronts sessions on Windows in production — everywhere else tmux
// exists and is the better answer. The unix implementation is real anyway,
// because it is what lets the daemon, the protocol and the client be tested on
// the machines this project is developed on.

// sessionShell is the login shell a new session runs.
func sessionShell() (string, []string) {
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh, []string{"-l"}
	}
	if p, err := exec.LookPath("sh"); err == nil {
		return p, []string{"-l"}
	}
	return "/bin/sh", []string{"-l"}
}

func sessionEnv() []string {
	env := os.Environ()
	out := env[:0]
	for _, kv := range env {
		if !strings.HasPrefix(kv, "TERM=") {
			out = append(out, kv)
		}
	}
	return append(out, "TERM=xterm-256color")
}

// hostDir maps a stored session path to one this process can start a shell in.
func hostDir(p string) string { return p }

// foregroundCommand names what is running in front of the session's shell: the
// shell's own name at a prompt, the launched program while one works. It is the
// local answer to tmux's pane_current_command.
func foregroundCommand(shellPID int) string {
	if shellPID <= 0 {
		return ""
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}
	type proc struct {
		ppid  int
		comm  string
		start uint64
	}
	procs := map[int]proc{}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", e.Name(), "stat"))
		if err != nil {
			continue
		}
		s := string(raw)
		// comm sits in parentheses and may itself contain either, so split on the
		// last close-paren.
		close := strings.LastIndexByte(s, ')')
		open := strings.IndexByte(s, '(')
		if open < 0 || close < open {
			continue
		}
		fields := strings.Fields(s[close+1:])
		if len(fields) < 20 {
			continue
		}
		ppid, _ := strconv.Atoi(fields[1])
		start, _ := strconv.ParseUint(fields[19], 10, 64)
		procs[pid] = proc{ppid: ppid, comm: s[open+1 : close], start: start}
	}

	self, ok := procs[shellPID]
	if !ok {
		return ""
	}
	// The foreground job is a direct child of the shell; with several, the most
	// recently started is the one the prompt handed control to.
	best := ""
	var bestStart uint64
	for pid, p := range procs {
		if p.ppid != shellPID || pid == shellPID {
			continue
		}
		if best == "" || p.start >= bestStart {
			best, bestStart = p.comm, p.start
		}
	}
	if best != "" {
		return best
	}
	return self.comm
}

// killTree ends the session's shell and everything it started. The shell is a
// session leader on its PTY, so signalling its process group reaches the tree.
func killTree(pid int) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
