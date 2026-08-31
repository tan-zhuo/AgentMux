//go:build windows

package update

import (
	"os"
	"os/exec"
	"syscall"
)

// Creation flags for a process that owes this one nothing — the same shape
// the natmux daemon is spawned with.
const (
	createNewProcessGroup = 0x00000200
	detachedProcess       = 0x00000008
)

// detach starts the relaunched app in its own process group with no console,
// so it survives this process exiting.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNewProcessGroup | detachedProcess,
	}
}

// Restart approximates exec(2), which Windows does not have: the new build is
// started detached with the same arguments and this process exits. The window
// where both exist means the successor can lose the port race; the published
// server build is Linux-only, so this path exists for hand-built binaries.
func Restart(path string) error {
	cmd := exec.Command(path, os.Args[1:]...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
