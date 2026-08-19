//go:build !windows

package update

import (
	"os/exec"
	"syscall"
)

// detach puts the relaunched app in its own session, so it is not dragged
// down by the exiting parent.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
