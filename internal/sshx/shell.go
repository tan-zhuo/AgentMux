package sshx

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
)

const (
	// scrollbackBytes is replayed into a re-mounted xterm so switching tabs or
	// restarting the UI does not blank the terminal.
	scrollbackBytes = 256 * 1024
	// flushInterval coalesces bursty output into a handful of events per second
	// instead of one per read.
	flushInterval = 8 * time.Millisecond
)

// ShellOptions describes a PTY to open on a server.
type ShellOptions struct {
	ServerID string            `json:"serverId"`
	Cols     int               `json:"cols"`
	Rows     int               `json:"rows"`
	Command  string            `json:"command"` // empty runs the user's login shell
	Cwd      string            `json:"cwd"`
	Env      map[string]string `json:"env"`
	Term     string            `json:"term"`
}

// ShellInfo is the frontend-visible handle for an open PTY.
type ShellInfo struct {
	ID       string `json:"id"`
	ServerID string `json:"serverId"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
	OpenedAt int64  `json:"openedAt"`
	Alive    bool   `json:"alive"`
}

// TermData is the payload of a term:data:<id> event.
type TermData struct {
	ID     string `json:"id"`
	Base64 string `json:"b64"`
}

// TermExit is the payload of a term:exit:<id> event.
type TermExit struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type shell struct {
	id       string
	serverID string
	sess     *ssh.Session
	stdin    io.WriteCloser
	lease    *Lease
	openedAt int64

	mu     sync.Mutex
	cols   int
	rows   int
	buf    []byte
	closed bool
}

// ShellManager owns every open PTY and streams their output to the frontend.
type ShellManager struct {
	pool *Pool
	emit func(name string, data any)

	mu     sync.Mutex
	shells map[string]*shell
}

// NewShellManager builds a manager that publishes output through emit.
func NewShellManager(pool *Pool, emit func(name string, data any)) *ShellManager {
	return &ShellManager{pool: pool, emit: emit, shells: map[string]*shell{}}
}

// Open starts a PTY session and begins streaming its output.
func (m *ShellManager) Open(opts ShellOptions) (ShellInfo, error) {
	if opts.ServerID == "" {
		return ShellInfo{}, errors.New("serverId is required")
	}
	if opts.Cols <= 0 {
		opts.Cols = 120
	}
	if opts.Rows <= 0 {
		opts.Rows = 32
	}
	if opts.Term == "" {
		opts.Term = "xterm-256color"
	}

	lease, err := m.pool.Acquire(opts.ServerID)
	if err != nil {
		return ShellInfo{}, err
	}

	sess, err := lease.Client.NewSession()
	if err != nil {
		lease.Release()
		return ShellInfo{}, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 38400,
		ssh.TTY_OP_OSPEED: 38400,
	}
	if err := sess.RequestPty(opts.Term, opts.Rows, opts.Cols, modes); err != nil {
		_ = sess.Close()
		lease.Release()
		return ShellInfo{}, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		_ = sess.Close()
		lease.Release()
		return ShellInfo{}, err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		_ = sess.Close()
		lease.Release()
		return ShellInfo{}, err
	}
	// With a PTY the remote side already merges stderr into stdout, but ask for
	// it anyway so pre-PTY failures are not swallowed.
	stderr, err := sess.StderrPipe()
	if err != nil {
		_ = sess.Close()
		lease.Release()
		return ShellInfo{}, err
	}

	sh := &shell{
		id:       uuid.NewString(),
		serverID: opts.ServerID,
		sess:     sess,
		stdin:    stdin,
		lease:    lease,
		openedAt: time.Now().Unix(),
		cols:     opts.Cols,
		rows:     opts.Rows,
		buf:      make([]byte, 0, 8*1024),
	}

	if err := m.start(sess, opts); err != nil {
		_ = sess.Close()
		lease.Release()
		return ShellInfo{}, err
	}

	m.mu.Lock()
	m.shells[sh.id] = sh
	m.mu.Unlock()

	go m.pump(sh, stdout)
	go m.drain(sh, stderr)
	go m.wait(sh)

	return ShellInfo{
		ID: sh.id, ServerID: sh.serverID, Cols: sh.cols, Rows: sh.rows,
		OpenedAt: sh.openedAt, Alive: true,
	}, nil
}

// start decides between a login shell and an explicit command. Environment and
// working directory are folded into the command rather than sent as SSH env
// vars, because most sshd configs reject AcceptEnv for anything but LANG.
func (m *ShellManager) start(sess *ssh.Session, opts ShellOptions) error {
	var prefix strings.Builder
	if len(opts.Env) > 0 {
		keys := make([]string, 0, len(opts.Env))
		for k := range opts.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			prefix.WriteString("export " + k + "=" + ShellQuote(opts.Env[k]) + "; ")
		}
	}
	if opts.Cwd != "" {
		prefix.WriteString("cd " + ShellQuote(opts.Cwd) + " || echo 'agentmux: cannot cd to " + opts.Cwd + "' >&2; ")
	}

	switch {
	case opts.Command != "":
		return sess.Start(prefix.String() + opts.Command)
	case prefix.Len() > 0:
		// Keep an interactive shell after applying cwd/env.
		return sess.Start(prefix.String() + `exec "${SHELL:-/bin/sh}" -l`)
	default:
		return sess.Shell()
	}
}

func (m *ShellManager) pump(sh *shell, r io.Reader) {
	var (
		pending []byte
		pmu     sync.Mutex
		done    = make(chan struct{})
	)

	flush := func() {
		pmu.Lock()
		if len(pending) == 0 {
			pmu.Unlock()
			return
		}
		chunk := pending
		pending = nil
		pmu.Unlock()

		sh.appendScrollback(chunk)
		m.emit("term:data:"+sh.id, TermData{ID: sh.id, Base64: base64.StdEncoding.EncodeToString(chunk)})
	}

	go func() {
		ticker := time.NewTicker(flushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				flush()
				return
			case <-ticker.C:
				flush()
			}
		}
	}()

	buf := make([]byte, 32*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			cp := make([]byte, n)
			copy(cp, buf[:n])
			pmu.Lock()
			pending = append(pending, cp...)
			pmu.Unlock()
		}
		if err != nil {
			close(done)
			return
		}
	}
}

func (m *ShellManager) drain(sh *shell, r io.Reader) {
	buf := make([]byte, 8*1024)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			cp := make([]byte, n)
			copy(cp, buf[:n])
			sh.appendScrollback(cp)
			m.emit("term:data:"+sh.id, TermData{ID: sh.id, Base64: base64.StdEncoding.EncodeToString(cp)})
		}
		if err != nil {
			return
		}
	}
}

func (m *ShellManager) wait(sh *shell) {
	err := sh.sess.Wait()
	reason := "session ended"
	if err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			reason = fmt.Sprintf("exited with status %d", exitErr.ExitStatus())
		} else {
			reason = err.Error()
		}
	}
	m.closeShell(sh.id, reason)
}

func (sh *shell) appendScrollback(chunk []byte) {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	sh.buf = append(sh.buf, chunk...)
	if len(sh.buf) > scrollbackBytes {
		excess := len(sh.buf) - scrollbackBytes
		// Trim forward to the next newline so the replay does not start
		// mid-escape-sequence.
		if idx := bytes.IndexByte(sh.buf[excess:], '\n'); idx >= 0 && excess+idx+1 <= len(sh.buf) {
			excess += idx + 1
		}
		sh.buf = append(sh.buf[:0], sh.buf[excess:]...)
	}
}

// Write forwards base64-encoded keystrokes from the frontend to the PTY.
func (m *ShellManager) Write(id, b64 string) error {
	sh, err := m.get(id)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return fmt.Errorf("bad payload: %w", err)
	}
	_, err = sh.stdin.Write(raw)
	return err
}

// Resize propagates an xterm resize to the remote PTY.
func (m *ShellManager) Resize(id string, cols, rows int) error {
	sh, err := m.get(id)
	if err != nil {
		return err
	}
	if cols <= 0 || rows <= 0 {
		return nil
	}
	sh.mu.Lock()
	sh.cols, sh.rows = cols, rows
	sh.mu.Unlock()
	return sh.sess.WindowChange(rows, cols)
}

// Scrollback returns the buffered output so a re-mounted terminal can replay it.
func (m *ShellManager) Scrollback(id string) (string, error) {
	sh, err := m.get(id)
	if err != nil {
		return "", err
	}
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return base64.StdEncoding.EncodeToString(sh.buf), nil
}

// Close ends a PTY. For a tmux-attached shell this is a detach: the remote tmux
// session and everything inside it keeps running.
func (m *ShellManager) Close(id string) error {
	return m.closeShell(id, "closed by user")
}

func (m *ShellManager) closeShell(id, reason string) error {
	m.mu.Lock()
	sh, ok := m.shells[id]
	if ok {
		delete(m.shells, id)
	}
	m.mu.Unlock()
	if !ok {
		return nil
	}

	sh.mu.Lock()
	already := sh.closed
	sh.closed = true
	sh.mu.Unlock()
	if already {
		return nil
	}

	_ = sh.stdin.Close()
	_ = sh.sess.Close()
	sh.lease.Release()
	m.emit("term:exit:"+id, TermExit{ID: id, Reason: reason})
	return nil
}

// CloseAll tears down every PTY, used on application shutdown.
func (m *ShellManager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.shells))
	for id := range m.shells {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.closeShell(id, "application shutting down")
	}
}

// List returns every live PTY.
func (m *ShellManager) List() []ShellInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ShellInfo, 0, len(m.shells))
	for _, sh := range m.shells {
		sh.mu.Lock()
		out = append(out, ShellInfo{
			ID: sh.id, ServerID: sh.serverID, Cols: sh.cols, Rows: sh.rows,
			OpenedAt: sh.openedAt, Alive: !sh.closed,
		})
		sh.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpenedAt < out[j].OpenedAt })
	return out
}

func (m *ShellManager) get(id string) (*shell, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sh, ok := m.shells[id]
	if !ok {
		return nil, fmt.Errorf("terminal %s is no longer open", id)
	}
	return sh, nil
}

// ShellQuote wraps a string in single quotes for safe interpolation into a
// remote /bin/sh command line.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
