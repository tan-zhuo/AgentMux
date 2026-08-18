//go:build !windows

package natmux

import (
	"os/exec"
	"syscall"
)

// spawnDaemon starts this executable as the session daemon, in its own session
// so it survives this process exiting — which is the entire point of it.
func spawnDaemon(exe string) error {
	cmd := exec.Command(exe, "--natmuxd")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap it if it ever exits; the daemon otherwise belongs to itself.
	go func() { _ = cmd.Wait() }()
	return nil
}
