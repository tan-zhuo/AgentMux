//go:build windows

package localx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"syscall"

	"agentmux/internal/sshx"

	"github.com/aymanbagabas/go-pty"
)

// nativeShell finds the PowerShell to use: PowerShell 7 when the user installed
// it, the built-in otherwise.
func nativeShell() string {
	if p, err := exec.LookPath("pwsh.exe"); err == nil {
		return p
	}
	return "powershell.exe"
}

// nativeShellCommand runs one PowerShell command line.
func nativeShellCommand(cmd string) (string, []string) {
	return nativeShell(), []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", cmd}
}

// hideConsole keeps one-shot commands from flashing console windows over a GUI
// process.
func hideConsole(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

func nativeAvailable() error {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		return errors.New("PowerShell was not found on PATH, which native Windows hosting needs")
	}
	return nil
}

func nativeHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not read the Windows profile directory: %w", err)
	}
	return filepath.ToSlash(home), nil
}

// nativeHostPath maps a stored forward-slash path to the backslash form Windows
// file APIs want. A bare drive ("C:") is pinned to its root, because without
// the slash Windows reads it as "wherever that drive's current directory is".
func nativeHostPath(p string) (string, error) {
	if len(p) == 2 && p[1] == ':' {
		p += "/"
	}
	return filepath.FromSlash(p), nil
}

// nativePathOf is the inverse: a Windows path in the slash form the rest of the
// application stores.
func nativePathOf(p string) string { return filepath.ToSlash(p) }

// NativeTerminals opens PTYs on this machine's native Windows side. It
// satisfies sshx.Opener, so the one shell manager serves it like any transport.
type NativeTerminals struct{ run *NativeRunner }

// NewNativeTerminals builds the native terminal opener.
func NewNativeTerminals(run *NativeRunner) *NativeTerminals { return &NativeTerminals{run: run} }

// OpenTerminal starts a ConPTY running PowerShell — interactive by default, or
// carrying one command. Working directory and environment are applied natively
// rather than folded into a command line, because unlike an SSH hop this
// process can simply set them.
func (t *NativeTerminals) OpenTerminal(opts sshx.ShellOptions) (sshx.Opened, error) {
	if err := t.run.Available(); err != nil {
		return sshx.Opened{}, err
	}
	p, err := pty.New()
	if err != nil {
		return sshx.Opened{}, fmt.Errorf("open a terminal: %w", err)
	}
	_ = p.Resize(opts.Cols, opts.Rows)

	args := []string{"-NoLogo"}
	if opts.Command != "" {
		args = append(args, "-Command", opts.Command)
	}
	cmd := p.Command(nativeShell(), args...)
	if opts.Cwd != "" {
		if dir, err := nativeHostPath(opts.Cwd); err == nil {
			cmd.Dir = dir
		}
	}
	env := os.Environ()
	keys := make([]string, 0, len(opts.Env))
	for k := range opts.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+opts.Env[k])
	}
	term := opts.Term
	if term == "" {
		term = "xterm-256color"
	}
	cmd.Env = append(env, "TERM="+term)

	if err := cmd.Start(); err != nil {
		_ = p.Close()
		return sshx.Opened{}, fmt.Errorf("start PowerShell: %w", err)
	}
	return sshx.Opened{
		Session: &nativeSession{pty: p, cmd: cmd},
		Stdout:  p,
	}, nil
}

// nativeSession is the local half of an open native terminal.
type nativeSession struct {
	pty pty.Pty
	cmd *pty.Cmd
}

func (s *nativeSession) Write(p []byte) (int, error) { return s.pty.Write(p) }

func (s *nativeSession) Resize(cols, rows int) error { return s.pty.Resize(cols, rows) }

func (s *nativeSession) Wait() error {
	err := s.cmd.Wait()
	if err == nil {
		return nil
	}
	if code := exitCode(err); code >= 0 {
		return fmt.Errorf("exited with status %d", code)
	}
	return err
}

func (s *nativeSession) Close() error {
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	return s.pty.Close()
}
