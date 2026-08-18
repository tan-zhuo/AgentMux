// Package localx runs commands, terminals and file operations on the computer
// AgentMux itself is running on, so that machine can be managed as a host like
// any other — same tree, same tmux sessions, same agents, no sshd and no
// credentials in between.
//
// The Runner/Files/Terminals trio speaks POSIX, because everything above it
// does: tmux session names, absolute paths, `sh -lc` command lines. On Linux
// and macOS that is the machine itself. On Windows it is WSL, which is where
// tmux — and therefore the promise that closing the window does not stop the
// work — lives on that platform.
//
// The Native* siblings in this package are the same machine's other face on
// Windows: PowerShell, Windows paths, Windows toolchains, for the work WSL
// cannot do. Their sessions persist through the natmux daemon instead of tmux.
package localx

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"agentmux/internal/sshx"
)

// Runner executes one-shot commands on this machine.
//
// It satisfies the same Runner interfaces as *sshx.Pool — tmux, agent detection
// and host metrics all reach a local host through this without knowing it is
// local.
type Runner struct {
	// probeOnce guards the one-time check that this platform can host anything.
	probeOnce sync.Once
	probeErr  error
}

// NewRunner builds a runner for this machine.
func NewRunner() *Runner { return &Runner{} }

// Available reports whether this machine can be managed at all, and why not when
// it cannot. On Windows that means: is there a WSL distribution to work in.
func (r *Runner) Available() error {
	r.probeOnce.Do(func() { r.probeErr = platformAvailable() })
	return r.probeErr
}

// Exec runs a command through the local POSIX shell and captures its output. The
// server id is accepted and ignored: there is only one local machine, and taking
// the argument is what lets this stand in for the SSH pool.
func (r *Runner) Exec(_ string, cmd string) (sshx.ExecResult, error) {
	if err := r.Available(); err != nil {
		return sshx.ExecResult{}, err
	}
	name, args := shellCommand(cmd)
	c := exec.Command(name, args...)
	var out, errBuf bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errBuf
	err := c.Run()
	res := sshx.ExecResult{Stdout: out.String(), Stderr: errBuf.String()}
	if err != nil {
		var exitErr *exec.ExitError
		// A non-zero exit is data here exactly as it is over SSH: tmux leans on
		// exit codes for existence checks.
		if errors.As(err, &exitErr) {
			res.Code = exitErr.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Probe answers the same question ServerService.Test asks of an SSH host: can
// this be worked on, and what is it.
func (r *Runner) Probe() sshx.Probe {
	start := time.Now()
	if err := r.Available(); err != nil {
		return sshx.Probe{Error: err.Error()}
	}
	p := sshx.Probe{OK: true}
	if res, err := r.Exec("", "uname -sr 2>/dev/null || sw_vers -productVersion"); err == nil {
		p.OS = strings.TrimSpace(res.Stdout)
	}
	if res, err := r.Exec("", "uptime"); err == nil {
		p.Uptime = strings.TrimSpace(res.Stdout)
	}
	if res, err := r.Exec("", "tmux -V"); err == nil && res.Code == 0 {
		p.HasTmux = true
		p.TmuxVer = strings.TrimSpace(res.Stdout)
	}
	p.LatencyMS = time.Since(start).Milliseconds()
	return p
}

// exitCode pulls the process status out of a wait error, or -1 when the error was
// something other than the process exiting.
func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// Home is the POSIX home directory of the account the local shell logs into. On
// Windows this is the WSL home, not the Windows profile: it is the directory the
// agents and their tmux sessions will actually see.
func (r *Runner) Home() (string, error) {
	res, err := r.Exec("", "printf %s \"$HOME\"")
	if err != nil {
		return "", err
	}
	home := strings.TrimSpace(res.Stdout)
	if home == "" {
		return "", fmt.Errorf("could not read the home directory of the local shell")
	}
	return home, nil
}
