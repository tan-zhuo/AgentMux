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

	// A terminal whose transport went away is dialled again rather than
	// declared dead. The waits back off from a second to a quarter of a minute
	// and stop after this many tries — about five minutes, which covers a
	// laptop lid, a wifi handover or a server reboot, and stops short of
	// hammering a machine that is gone for the day.
	reconnectTries      = 20
	reconnectFirstDelay = time.Second
	reconnectMaxDelay   = 15 * time.Second
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
	// WindowsHost marks a host whose shell is PowerShell rather than a POSIX
	// sh, so the command line is composed in the right dialect. Set by the
	// application layer, which knows the host's kind; never by the frontend.
	WindowsHost bool `json:"-"`
	// OneShot marks a terminal that exists to run one command and is finished
	// when that command is. A dropped connection ends it instead of dialling
	// back, because dialling back would mean running the command again — and
	// re-running an install or a build because the wifi blinked is a side
	// effect nobody asked for. A shell or a session attach has no such
	// hazard: there is nothing to repeat, only something to rejoin.
	OneShot bool `json:"-"`
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

// TermReconnect is the payload of a term:reconnect:<id> event: one attempt at
// putting a dropped terminal back, or the moment it succeeded.
type TermReconnect struct {
	ID      string `json:"id"`
	Attempt int    `json:"attempt"`
	Of      int    `json:"of"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail"`
}

// ExitError reports a terminal that ended by itself — the command finished, or
// the shell was exited, or a tmux client was detached.
//
// It is the one ending that is never retried: the session did what it was asked
// and stopped. Everything else — a closed socket, a timed-out keepalive, a
// vanished network — is a transport failure, and those are worth dialling again.
type ExitError struct{ Code int }

func (e *ExitError) Error() string { return fmt.Sprintf("exited with status %d", e.Code) }

// Reopener starts the same terminal again after its transport went away. It is
// handed the options the terminal was opened with, carrying whatever size the
// window has since been resized to.
type Reopener func(opts ShellOptions) (Opened, error)

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
	openedAt int64
	// opts and reopen are what a reconnect needs: what this terminal was, and
	// how to start it again. A nil reopen means the transport cannot be
	// redialled — a local PTY, whose process ending is the whole story.
	opts   ShellOptions
	reopen Reopener
	done   chan struct{}

	mu sync.Mutex
	// sess and release are swapped when a dropped terminal comes back, so both
	// live under the lock. sess is nil for the moment in between.
	sess    Session
	release func()
	cols    int
	rows    int
	buf     []byte
	closed  bool
}

// session returns the live half of the terminal, or nil while it is being
// redialled.
func (sh *shell) session() Session {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.sess
}

func (sh *shell) isClosed() bool {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return sh.closed
}

// currentOpts is what this terminal was opened with, at the size the window has
// now — a reconnect that came up at the original size would need a resize the
// remote program has no reason to expect.
func (sh *shell) currentOpts() ShellOptions {
	sh.mu.Lock()
	defer sh.mu.Unlock()
	o := sh.opts
	o.Cols, o.Rows = sh.cols, sh.rows
	return o
}

// retryable reports whether an ending is worth dialling again.
func (sh *shell) retryable(err error) bool {
	if sh.reopen == nil || err == nil || sh.isClosed() {
		return false
	}
	var exit *ExitError
	return !errors.As(err, &exit)
}

// dropTransport hands back everything the dead session held. The pool counts
// leases, and a lease never released is a connection never reaped.
func (sh *shell) dropTransport() {
	sh.mu.Lock()
	sess, release := sh.sess, sh.release
	sh.sess, sh.release = nil, nil
	sh.mu.Unlock()
	if sess != nil {
		_ = sess.Close()
	}
	if release != nil {
		release()
	}
}

func (sh *shell) adoptTransport(opened Opened) {
	sh.mu.Lock()
	sh.sess, sh.release = opened.Session, opened.Release
	sh.mu.Unlock()
}

// ShellManager owns every open PTY and streams their output to the frontend.
type ShellManager struct {
	pool *Pool
	emit func(name string, data any)

	// localFor, when set, picks a local transport for a host. Returning nil
	// means the host is not local and is reached over SSH.
	localFor func(serverID string) Opener

	mu     sync.Mutex
	shells map[string]*shell
}

// NewShellManager builds a manager that publishes output through emit.
func NewShellManager(pool *Pool, emit func(name string, data any)) *ShellManager {
	return &ShellManager{pool: pool, emit: emit, shells: map[string]*shell{}}
}

// UseLocal teaches the manager to open terminals on this machine. The picker
// answers per host — this machine has more than one local flavour on Windows,
// where the same computer is both a WSL host and a native one. Called once at
// startup; without it every host is an SSH host, which is what every build
// before local hosts existed did.
func (m *ShellManager) UseLocal(pick func(serverID string) Opener) {
	m.localFor = pick
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

	opened, redialable, err := m.open(opts)
	if err != nil {
		return ShellInfo{}, err
	}
	var reopen Reopener
	if redialable {
		reopen = m.openSSH
	}
	return m.adopt(opts, opened, reopen), nil
}

// Adopt registers a terminal some transport already opened and starts streaming
// it, exactly as Open does for the transports the manager dials itself. It is
// how a native session attach — opened against the local session daemon, not
// through an Opener — joins the same scrollback and event machinery.
func (m *ShellManager) Adopt(opts ShellOptions, opened Opened) ShellInfo {
	return m.adopt(opts, opened, nil)
}

// AdoptWith is Adopt for a transport that can start the terminal again. The
// session daemon's attachments arrive this way: an attach that drops is worth
// redialling for exactly the same reason an SSH one is, and the session it was
// attached to is still there waiting.
func (m *ShellManager) AdoptWith(opts ShellOptions, opened Opened, reopen Reopener) ShellInfo {
	return m.adopt(opts, opened, reopen)
}

func (m *ShellManager) adopt(opts ShellOptions, opened Opened, reopen Reopener) ShellInfo {
	if opts.Cols <= 0 {
		opts.Cols = 120
	}
	if opts.Rows <= 0 {
		opts.Rows = 32
	}
	sh := &shell{
		id:       uuid.NewString(),
		serverID: opts.ServerID,
		openedAt: time.Now().Unix(),
		opts:     opts,
		reopen:   reopen,
		done:     make(chan struct{}),
		sess:     opened.Session,
		release:  opened.Release,
		cols:     opts.Cols,
		rows:     opts.Rows,
		buf:      make([]byte, 0, 8*1024),
	}

	m.mu.Lock()
	m.shells[sh.id] = sh
	m.mu.Unlock()

	m.stream(sh, opened)
	go m.watch(sh)

	return ShellInfo{
		ID: sh.id, ServerID: sh.serverID, Cols: sh.cols, Rows: sh.rows,
		OpenedAt: sh.openedAt, Alive: true,
	}
}

// stream starts the readers for one transport. It runs again on every
// reconnect, against the new streams, while the shell — its id, its scrollback,
// the tab watching it — stays exactly where it was.
func (m *ShellManager) stream(sh *shell, opened Opened) {
	go m.pump(sh, opened.Stdout)
	if opened.Stderr != nil {
		go m.drain(sh, opened.Stderr)
	}
}

// open routes to the transport that owns this host, and says whether that
// transport is one a dropped terminal can be re-opened on. Only the network
// ones are: a local PTY that ended, ended because its process did.
func (m *ShellManager) open(opts ShellOptions) (Opened, bool, error) {
	if m.localFor != nil {
		if local := m.localFor(opts.ServerID); local != nil {
			opened, err := local.OpenTerminal(opts)
			return opened, false, err
		}
	}
	opened, err := m.openSSH(opts)
	return opened, err == nil && !opts.OneShot, err
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
	// that is the request sshd handles best; anything else is a command line —
	// composed in the dialect the host's shell actually speaks.
	line := CommandLine(opts)
	if opts.WindowsHost {
		line = WinCommandLine(opts)
	}
	if line == "" {
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
		return &ExitError{Code: exitErr.ExitStatus()}
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

// watch follows one terminal for as long as it lives, across however many
// transports that takes.
//
// A terminal that ends on its own terms is over. One whose transport went out
// from under it is not: the tmux session on the far side is still running, the
// agent inside it is still working, and the only thing that actually broke was
// the pipe. So the pipe is rebuilt, under the same id, and the loop goes back
// to waiting.
func (m *ShellManager) watch(sh *shell) {
	for {
		sess := sh.session()
		if sess == nil {
			return
		}
		// Each transport phrases its own ending, because only it knows what an
		// exit status meant.
		err := sess.Wait()
		reason := "session ended"
		if err != nil {
			reason = err.Error()
		}
		if !sh.retryable(err) {
			if !sh.isClosed() {
				_ = m.closeShell(sh.id, reason)
			}
			return
		}
		if !m.reconnect(sh, reason) {
			return
		}
	}
}

// reconnect dials a dropped terminal back up, and reports whether it is live
// again. Giving up ends the terminal the ordinary way, so the pane falls back
// to the button it has always had.
func (m *ShellManager) reconnect(sh *shell, reason string) bool {
	m.note(sh, reason+" — reconnecting…")
	delay := reconnectFirstDelay

	for attempt := 1; attempt <= reconnectTries; attempt++ {
		// The old transport is let go before dialling, not after: the pool
		// counts leases, and it will not redial a server while the dead
		// connection to it is still spoken for.
		sh.dropTransport()
		if !m.pause(sh, delay) {
			return false
		}
		if delay *= 2; delay > reconnectMaxDelay {
			delay = reconnectMaxDelay
		}

		opts := sh.currentOpts()
		opened, err := sh.reopen(opts)
		if err != nil {
			m.emit("term:reconnect:"+sh.id, TermReconnect{
				ID: sh.id, Attempt: attempt, Of: reconnectTries, Detail: err.Error(),
			})
			continue
		}
		// The tab may have been closed while that dial was in flight, in which
		// case nobody is watching this terminal and it must not be left open.
		if sh.isClosed() {
			_ = opened.Session.Close()
			if opened.Release != nil {
				opened.Release()
			}
			return false
		}

		sh.adoptTransport(opened)
		m.stream(sh, opened)
		_ = opened.Session.Resize(opts.Cols, opts.Rows)
		m.note(sh, "reconnected")
		m.emit("term:reconnect:"+sh.id, TermReconnect{
			ID: sh.id, Attempt: attempt, Of: reconnectTries, OK: true,
		})
		return true
	}

	_ = m.closeShell(sh.id, fmt.Sprintf("%s — gave up after %d attempts", reason, reconnectTries))
	return false
}

// pause waits out a backoff, and reports false if the terminal was closed while
// it waited. Closing a tab must not be answered by one more reconnect.
func (m *ShellManager) pause(sh *shell, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-sh.done:
		return false
	case <-timer.C:
		return !sh.isClosed()
	}
}

// note writes a line of AgentMux's own voice into the terminal, dimmed so it
// cannot be mistaken for output from the far end. It goes through the same path
// as real output, which is what puts it in the scrollback too — a terminal
// scrolled back to hours ago still says where the gap came from.
func (m *ShellManager) note(sh *shell, text string) {
	line := []byte("\r\n\x1b[38;5;245m── " + text + " ──\x1b[0m\r\n")
	sh.appendScrollback(line)
	m.emit("term:data:"+sh.id, TermData{ID: sh.id, Base64: base64.StdEncoding.EncodeToString(line)})
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
	sess := sh.session()
	if sess == nil {
		return fmt.Errorf("terminal %s is reconnecting", id)
	}
	_, err = sess.Write(raw)
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
	// The size is remembered even while nothing is attached, because it is what
	// a reconnect will ask the new PTY for.
	sh.mu.Lock()
	sh.cols, sh.rows = cols, rows
	sess := sh.sess
	sh.mu.Unlock()
	if sess == nil {
		return nil
	}
	return sess.Resize(cols, rows)
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
	// Closing done stops a reconnect that is mid-backoff. Without it, a tab
	// closed while its terminal was dropping would be answered by a redial
	// nobody asked for and nobody is watching.
	if sh.done != nil {
		close(sh.done)
	}

	sh.dropTransport()
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
