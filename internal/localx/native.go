package localx

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"agentmux/internal/sshx"
)

// NativeRunner executes one-shot commands on this machine's native Windows
// side: PowerShell, Windows paths, Windows toolchains. It exists because some
// work cannot happen inside WSL — MSVC builds, WPF, running the .exe that was
// just built — and it satisfies the same Runner interfaces as *Runner and
// *sshx.Pool, so everything above reaches it without knowing what it is.
//
// On every other platform it reports itself unavailable: the machine itself is
// already the native host there, and KindLocal covers it.
type NativeRunner struct {
	probeOnce sync.Once
	probeErr  error
}

// NewNativeRunner builds a runner for this machine's native Windows side.
func NewNativeRunner() *NativeRunner { return &NativeRunner{} }

// Available reports whether native hosting exists on this platform.
func (r *NativeRunner) Available() error {
	r.probeOnce.Do(func() { r.probeErr = nativeAvailable() })
	return r.probeErr
}

// Exec runs a command through PowerShell and captures its output. The server id
// is accepted and ignored, exactly as it is on the POSIX local runner.
func (r *NativeRunner) Exec(_ string, cmd string) (sshx.ExecResult, error) {
	if err := r.Available(); err != nil {
		return sshx.ExecResult{}, err
	}
	name, args := nativeShellCommand(cmd)
	c := exec.Command(name, args...)
	hideConsole(c)
	var out, errBuf bytes.Buffer
	c.Stdout = &out
	c.Stderr = &errBuf
	err := c.Run()
	res := sshx.ExecResult{Stdout: out.String(), Stderr: errBuf.String()}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.Code = exitErr.ExitCode()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Probe answers the same question ServerService.Test asks of any host: can this
// be worked on, and what is it.
func (r *NativeRunner) Probe() sshx.Probe {
	start := time.Now()
	if err := r.Available(); err != nil {
		return sshx.Probe{Error: err.Error()}
	}
	p := sshx.Probe{OK: true}
	if res, err := r.Exec("", `(Get-CimInstance Win32_OperatingSystem).Caption`); err == nil {
		p.OS = strings.TrimSpace(res.Stdout)
	}
	if res, err := r.Exec("",
		`$b=(Get-CimInstance Win32_OperatingSystem).LastBootUpTime; `+
			`'{0:%d}d {0:hh}h {0:mm}m' -f ((Get-Date)-$b)`); err == nil {
		p.Uptime = strings.TrimSpace(res.Stdout)
	}
	// Sessions here persist through AgentMux's own daemon rather than tmux; for
	// the one question the probe answers — can agents keep running after the
	// window closes — the truthful answer is yes.
	p.HasTmux = true
	p.TmuxVer = "AgentMux sessions"
	p.LatencyMS = time.Since(start).Milliseconds()
	return p
}

// Home is the Windows profile directory, in the forward-slash form every path
// in this application uses.
func (r *NativeRunner) Home() (string, error) {
	if err := r.Available(); err != nil {
		return "", err
	}
	return nativeHome()
}
