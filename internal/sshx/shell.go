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

// Session is one transport's half of an open terminal: keystrokes in, a size, and
// an end.
//
// The SSH implementation is in this file. A local one — processes on the machine
// AgentMux is running on — is injected by the application, which is what lets one
// manager, one scrollback and one set of events serve both without either
// transport knowing about the other.
type Session interface {
	io.Writer
	Resize(cols, rows int) error
	// Wait blocks until the terminal ends and returns why, phrased for a person.
	Wait() error
	Close() error
}

// Opened is a started terminal: the session that drives it and the streams to
// read from it.
type Opened struct {
	Session Session
	Stdout  io.Reader
	// Stderr is optional. A PTY normally merges it into Stdout.
	Stderr io.Reader
	// Release returns whatever the transport borrowed to open this, once the
	// terminal has ended.
	Release func()
}

// Opener starts terminals for one transport.
type Opener interface {
	OpenTerminal(opts ShellOptions) (Opened, error)
}

type shell struct {
	id       string
	serverID string
	sess     Session
	release  func()
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

	// local, when set, opens terminals for hosts isLocal reports as local.
	local   Opener
	isLocal func(serverID string) bool

	mu     sync.Mutex
	shells map[string]*shell
}

// NewShellManager builds a manager that publishes output through emit.
func NewShellManager(pool *Pool, emit func(name string, data any)) *ShellManager {
	return &ShellManager{pool: pool, emit: emit, shells: map[string]*shell{}}
}

// UseLocal teaches the manager to open terminals on this machine for the hosts
// isLocal claims. Called once at startup; without it every host is an SSH host,
// which is what every build before local hosts existed did.
func (m *ShellManager) UseLocal(isLocal func(serverID string) bool, open Opener) {
	m.isLocal = isLocal
	m.local = open
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

	opened, err := m.open(opts)
	if err != nil {
		return ShellInfo{}, err
	}

	sh := &shell{
		id:       uuid.NewString(),
		serverID: opts.ServerID,
		sess:     opened.Session,
		release:  opened.Release,
		openedAt: time.Now().Unix(),
		cols:     opts.Cols,
		rows:     opts.Rows,
		buf:      make([]byte, 0, 8*1024),
	}

	m.mu.Lock()
	m.shells[sh.id] = sh
	m.mu.Unlock()

	go m.pump(sh, opened.Stdout)
	if opened.Stderr != nil {
		go m.drain(sh, opened.Stderr)
	}
	go m.wait(sh)

	return ShellInfo{
		ID: sh.id, ServerID: sh.serverID, Cols: sh.cols, Rows: sh.rows,
		OpenedAt: sh.openedAt, Alive: true,
	}, nil
}

// open routes to the transport that owns this host.
func (m *ShellManager) open(opts ShellOptions) (Opened, error) {
	if m.local != nil && m.isLocal != nil && m.isLocal(opts.ServerID) {
		return m.local.OpenTerminal(opts)
	}
	return m.openSSH(opts)
}

// openSSH starts a PTY session on a remote host.
func (m *ShellManager) openSSH(opts ShellOptions) (Opened, error) {
	lease, err := m.pool.Acquire(opts.ServerID)
	if err != nil {
		return Opened{}, err
	}

	sess, err := lease.Client.NewSession()
	if err != nil {
		lease.Release()
		return Opened{}, err
	}
	fail := func(err error) (Opened, error) {
		_ = sess.Close()
		lease.Release()
		return Opened{}, err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 38400,
		ssh.TTY_OP_OSPEED: 38400,
	}
	if err := sess.RequestPty(opts.Term, opts.Rows, opts.Cols, modes); err != nil {
		return fail(fmt.Errorf("request pty: %w", err))
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		return fail(err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return fail(err)
	}
	// With a PTY the remote side already merges stderr into stdout, but ask for
	// it anyway so pre-PTY failures are not swallowed.
	stderr, err := sess.StderrPipe()
	if err != nil {
		return fail(err)
	}

	// A login shell is asked for as a shell rather than as a command, because
	// that is the request sshd handles best; anything else is a command line.
	if line := CommandLine(opts); line == "" {
		err = sess.Shell()
	} else {
		err = sess.Start(line)
	}
	if err != nil {
		return fail(err)
	}

	return Opened{
		Session: &sshSession{sess: sess, stdin: stdin},
		Stdout:  stdout,
		Stderr:  stderr,
		Release: lease.Release,
	}, nil
}

// sshSession is the SSH half of an open terminal.
type sshSession struct {
	sess  *ssh.Session
	stdin io.WriteCloser
}

func (s *sshSession) Write(p []byte) (int, error) { return s.stdin.Write(p) }

func (s *sshSession) Resize(cols, rows int) error { return s.sess.WindowChange(rows, cols) }

func (s *sshSession) Wait() error {
	err := s.sess.Wait()
	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return fmt.Errorf("exited with status %d", exitErr.ExitStatus())
	}
	return err
}

func (s *sshSession) Close() error {
	_ = s.stdin.Close()
	return s.sess.Close()
}

// CommandLine folds the working directory and environment of a terminal into one
// POSIX command line, and reports "" when a plain login shell is all that was
// asked for.
//
// They are folded into the command rather than sent as SSH environment requests
// because most sshd configs reject AcceptEnv for anything but LANG — and because
// a command line is the one thing every transport here can start.
func CommandLine(opts ShellOptions) string {
	var line strings.Builder
	if len(opts.Env) > 0 {
		keys := make([]string, 0, len(opts.Env))
		for k := range opts.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			line.WriteString("export " + k + "=" + ShellQuote(opts.Env[k]) + "; ")
		}
	}
	if opts.Cwd != "" {
		line.WriteString("cd " + ShellQuote(opts.Cwd) + " || echo 'agentmux: cannot cd to " + opts.Cwd + "' >&2; ")
	}

	switch {
	case opts.Command != "":
		line.WriteString(opts.Command)
	case line.Len() > 0:
		// Keep an interactive shell after applying cwd and environment.
		line.WriteString(`exec "${SHELL:-/bin/sh}" -l`)
	default:
		return ""
	}
	return line.String()
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
	// Each transport phrases its own ending, because only it knows what an exit
	// status meant.
	reason := "session ended"
	if err := sh.sess.Wait(); err != nil {
		reason = err.Error()
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
	_, err = sh.sess.Write(raw)
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
	return sh.sess.Resize(cols, rows)
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

	_ = sh.sess.Close()
	if sh.release != nil {
		sh.release()
	}
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
