//go:build !windows

package update

import (
	"os"
	"os/exec"
	"syscall"
)

// detach puts the relaunched app in its own session, so it is not dragged
// down by the exiting parent.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// Restart replaces this process with the executable at path, keeping the
// arguments and environment it was started with. It does not return on
// success. This is how a server hands itself over to a new build: Go opens
// sockets close-on-exec, so the listening port is free the instant the new
// image starts, and a supervisor — systemd, a wrapper script — sees the same
// PID carry on rather than an exit to react to.
func Restart(path string) error {
	return syscall.Exec(path, os.Args, os.Environ())
}
