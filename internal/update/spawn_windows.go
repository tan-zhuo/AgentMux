//go:build windows

package update

import (
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
