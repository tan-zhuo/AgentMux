//go:build windows

package natmux

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// sessionShell is the shell a new session runs: PowerShell, because this host
// kind exists precisely for work that needs Windows itself — MSVC, WPF, running
// the .exe that was just built. pwsh (PowerShell 7) is preferred when the user
// installed it; every Windows has powershell.exe.
func sessionShell() (string, []string) {
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		return p, []string{"-NoLogo"}
	}
	return "powershell.exe", []string{"-NoLogo"}
}

func sessionEnv() []string {
	// ConPTY translates the console's output itself; TERM is for the programs
	// inside, many of which are the same CLIs people run under tmux.
	return append(os.Environ(), "TERM=xterm-256color")
}

// hostDir maps a stored session path — kept in forward-slash form like every
// other path in this application — back to the backslash form CreateProcess
// wants for a working directory.
func hostDir(p string) string { return filepath.FromSlash(p) }

// foregroundCommand names what is running in front of the session's shell,
// answering what tmux's pane_current_command answers: the shell's own name at a
// prompt, the launched program while one works. Resolved from a process
// snapshot, because ConPTY has no notion of a foreground process group.
func foregroundCommand(shellPID int) string {
	if shellPID <= 0 {
		return ""
	}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snap)

	type proc struct {
		ppid int
		exe  string
	}
	procs := map[int]proc{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	if windows.Process32First(snap, &e) == nil {
		for {
			procs[int(e.ProcessID)] = proc{
				ppid: int(e.ParentProcessID),
				exe:  windows.UTF16ToString(e.ExeFile[:]),
			}
			if windows.Process32Next(snap, &e) != nil {
				break
			}
		}
	}

	self, ok := procs[shellPID]
	if !ok {
		return ""
	}
	// The last direct child that is not console plumbing is the running job.
	// The snapshot has no start times, so "last" is approximated by taking the
	// final match in PID order — good enough for the one question asked of it:
	// is something other than the shell running here.
	best := ""
	for pid, p := range procs {
		if p.ppid != shellPID || pid == shellPID {
			continue
		}
		name := commandName(p.exe)
		if name == "conhost" || name == "openconsole" {
			continue
		}
		best = name
	}
	if best != "" {
		return best
	}
	return commandName(self.exe)
}

// commandName strips the .exe suffix and lowercases, so "PowerShell.EXE" and
// the "powershell" the status logic compares against are the same word.
func commandName(exe string) string {
	name := strings.ToLower(filepath.Base(exe))
	return strings.TrimSuffix(name, ".exe")
}

// killTree ends the session's shell and everything it started. taskkill /T is
// the one built-in that walks the tree.
func killTree(pid int) {
	if pid <= 0 {
		return
	}
	cmd := exec.Command("taskkill.exe", "/T", "/F", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Run()
}
