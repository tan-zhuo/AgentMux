//go:build windows

package natmux

import (
	"os/exec"
	"syscall"
)

// Creation flags for a process that owes this one nothing: no console window,
// its own process group, detached from any console we might have.
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// spawnDaemon starts this executable as the session daemon, detached, so the
// sessions it holds outlive the window that asked for them.
func spawnDaemon(exe string) error {
	cmd := exec.Command(exe, "--natmuxd")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNewProcessGroup | detachedProcess,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
