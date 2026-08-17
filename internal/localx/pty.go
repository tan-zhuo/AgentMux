package localx

import (
	"fmt"
	"io"
	"os"
	"sort"
	"sync"

	"agentmux/internal/sshx"

	"github.com/aymanbagabas/go-pty"
)

// Terminals opens PTYs on this machine. It satisfies sshx.Opener, which is how the
// one shell manager — with its scrollback, its coalescing and its exit events —
// serves local hosts without knowing they are local.
type Terminals struct{ run *Runner }

// NewTerminals builds the local terminal opener.
func NewTerminals(run *Runner) *Terminals { return &Terminals{run: run} }

// OpenTerminal starts a local PTY running either the user's login shell or the
// command the caller asked for — which, for an agent or a tmux tab, is a tmux
// attach. Closing it detaches: the session keeps running under the local tmux
// server, exactly as it does on a remote host.
func (t *Terminals) OpenTerminal(opts sshx.ShellOptions) (sshx.Opened, error) {
	if err := t.run.Available(); err != nil {
		return sshx.Opened{}, err
	}

	p, err := pty.New()
	if err != nil {
		return sshx.Opened{}, fmt.Errorf("open a local terminal: %w", err)
	}
	if err := p.Resize(opts.Cols, opts.Rows); err != nil {
		// Not fatal: the size is corrected by the first resize from the UI.
		_ = err
	}

	name, args := terminalCommand(sshx.CommandLine(opts))
	cmd := p.Command(name, args...)
	cmd.Env = terminalEnv(opts)

	if err := cmd.Start(); err != nil {
		_ = p.Close()
		return sshx.Opened{}, fmt.Errorf("start %s: %w", name, err)
	}

	return sshx.Opened{
		Session: &localSession{pty: p, cmd: cmd},
		// A PTY merges the two streams by construction, so there is no second
		// reader to drain.
		Stdout: p,
	}, nil
}

// terminalEnv is this process's environment with the terminal type set and the
// per-workspace variables applied.
//
// The variables are also folded into the command line by sshx.CommandLine, which
// is what a remote host needs; setting them here as well costs nothing and means
// a local terminal has them even in the login-shell case.
func terminalEnv(opts sshx.ShellOptions) []string {
	env := make([]string, 0, len(os.Environ())+len(opts.Env)+1)
	for _, kv := range os.Environ() {
		// TERM is set from opts below; anything inherited would fight it.
		if len(kv) > 5 && kv[:5] == "TERM=" {
			continue
		}
		env = append(env, kv)
	}
	term := opts.Term
	if term == "" {
		term = "xterm-256color"
	}
	env = append(env, "TERM="+term)

	keys := make([]string, 0, len(opts.Env))
	for k := range opts.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+opts.Env[k])
	}
	return env
}

// localSession is the local half of an open terminal.
type localSession struct {
	pty pty.Pty
	cmd *pty.Cmd

	once sync.Once
}

func (s *localSession) Write(p []byte) (int, error) { return s.pty.Write(p) }

func (s *localSession) Resize(cols, rows int) error { return s.pty.Resize(cols, rows) }

func (s *localSession) Wait() error {
	err := s.cmd.Wait()
	if err == nil {
		return nil
	}
	if code := exitCode(err); code >= 0 {
		return fmt.Errorf("exited with status %d", code)
	}
	return err
}

// Close ends the terminal. The process is killed rather than asked to leave,
// because for the case that matters — a tmux client — killing the client is
// precisely what detaching is, and the session it was attached to keeps running.
func (s *localSession) Close() error {
	var err error
	s.once.Do(func() {
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		err = s.pty.Close()
	})
	return err
}

// A pty is an io.ReadWriteCloser, which is what lets it serve directly as the
// terminal's output stream above. Asserted here so a change in the dependency is
// a compile error rather than a surprise at runtime.
var _ io.ReadWriteCloser = pty.Pty(nil)
