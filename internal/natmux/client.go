package natmux

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"agentmux/internal/sshx"
	"agentmux/internal/tmuxx"
)

// Transport carries the protocol to a daemon, wherever it is. The local one
// dials the pipe and spawns the daemon on a miss; the remote one reaches a
// loopback port on another machine through an SSH port forward and installs
// the daemon there first when nothing answers.
type Transport interface {
	// Dial opens one protocol connection, plus a release for whatever the
	// transport borrowed to open it (an SSH lease, typically).
	Dial() (io.ReadWriteCloser, func(), error)
	// Ensure makes the daemon reachable: spawn it, or deploy and start it.
	Ensure() error
	// Token is what a TCP connection must present; "" means the transport's own
	// access control (the pipe's ACL) already answers who is calling.
	Token() (string, error)
}

// localTransport reaches the daemon on this machine.
type localTransport struct {
	mu sync.Mutex
}

func (t *localTransport) Dial() (io.ReadWriteCloser, func(), error) {
	conn, err := dial(2 * time.Second)
	if err != nil {
		return nil, nil, err
	}
	return conn, func() {}, nil
}

func (t *localTransport) Token() (string, error) { return "", nil }

// Ensure spawns the daemon when nothing answers. Serialised so a burst of calls
// cannot race a dozen daemons into existence.
func (t *localTransport) Ensure() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if conn, err := dial(500 * time.Millisecond); err == nil {
		_ = conn.Close()
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot locate the AgentMux executable to start the session daemon: %w", err)
	}
	if err := spawnDaemon(exe); err != nil {
		return fmt.Errorf("start the session daemon: %w", err)
	}
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if conn, err := dial(500 * time.Millisecond); err == nil {
			_ = conn.Close()
			return nil
		}
	}
	return errors.New("the session daemon did not come up; see its log next to the application data")
}

// Client speaks to a session daemon, starting it first when nothing answers.
// Its methods mirror the tmux client's, shaped in the same types, because the
// application above routes to one or the other per host and must not care
// which one it got. The serverID every method takes is accepted and ignored:
// which machine this client reaches is fixed by its transport.
type Client struct {
	t Transport
}

// NewClient builds a client for this machine. Nothing is dialled until
// something is asked.
func NewClient() *Client { return &Client{t: &localTransport{}} }

// NewRemoteClient builds a client for a daemon somewhere else, reached through
// the given transport.
func NewRemoteClient(t Transport) *Client { return &Client{t: t} }

// open dials, falling back to Ensure on a miss, and authenticates when the
// transport requires it. The returned scanner is already sized for protocol
// lines and must be used for every read on the connection.
func (c *Client) open() (io.ReadWriteCloser, func(), *bufio.Scanner, error) {
	conn, release, err := c.t.Dial()
	if err != nil {
		if err := c.t.Ensure(); err != nil {
			return nil, nil, nil, err
		}
		conn, release, err = c.t.Dial()
		if err != nil {
			return nil, nil, nil, err
		}
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), maxLine)

	token, err := c.t.Token()
	if err != nil {
		release()
		_ = conn.Close()
		return nil, nil, nil, err
	}
	if token != "" {
		if err := roundTrip(conn, sc, request{Op: "auth", Token: token}); err != nil {
			release()
			_ = conn.Close()
			return nil, nil, nil, fmt.Errorf("authenticate to the session daemon: %w", err)
		}
	}
	return conn, release, sc, nil
}

// roundTrip sends one request and demands an ok.
func roundTrip(conn io.Writer, sc *bufio.Scanner, req request) error {
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	if !sc.Scan() {
		return errors.New("the session daemon closed the connection")
	}
	var res response
	if err := json.Unmarshal(sc.Bytes(), &res); err != nil {
		return err
	}
	if res.Error != "" {
		return errors.New(res.Error)
	}
	return nil
}

// call runs one request over a fresh connection. Dials are cheap on both
// transports — a pipe, or a channel on an SSH connection that is already up —
// and one connection per call means no cross-talk between concurrent callers.
func (c *Client) call(req request) (response, error) {
	conn, release, sc, err := c.open()
	if err != nil {
		return response{}, err
	}
	defer func() {
		_ = conn.Close()
		release()
	}()
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return response{}, err
	}
	if !sc.Scan() {
		return response{}, errors.New("the session daemon closed the connection")
	}
	var res response
	if err := json.Unmarshal(sc.Bytes(), &res); err != nil {
		return response{}, err
	}
	if res.Error != "" {
		return res, errors.New(res.Error)
	}
	return res, nil
}

// Available reports whether sessions can be hosted here. Unlike tmux this is
// not a question about an installed binary — the daemon is this executable —
// so unavailability means the daemon could not be started or spoken to.
func (c *Client) Available(_ string) tmuxx.Info {
	res, err := c.call(request{Op: "ping"})
	if err != nil {
		return tmuxx.Info{Error: err.Error()}
	}
	return tmuxx.Info{Available: true, Version: "AgentMux sessions v" + res.Version}
}

// ListSessions returns every session the daemon holds.
func (c *Client) ListSessions(_ string) ([]tmuxx.Session, error) {
	res, err := c.call(request{Op: "list"})
	if err != nil {
		return nil, err
	}
	out := make([]tmuxx.Session, 0, len(res.Sessions))
	for _, s := range res.Sessions {
		out = append(out, tmuxx.Session{
			Name:     s.Name,
			Windows:  1,
			Attached: s.Attached > 0,
			Created:  s.Created,
			Activity: s.Activity,
		})
	}
	return out, nil
}

// ListPanes returns one pane per session: a native session has exactly one
// terminal, and the pane is how everything above addresses it.
func (c *Client) ListPanes(_ string) ([]tmuxx.Pane, error) {
	res, err := c.call(request{Op: "list"})
	if err != nil {
		return nil, err
	}
	out := make([]tmuxx.Pane, 0, len(res.Sessions))
	for _, s := range res.Sessions {
		out = append(out, tmuxx.Pane{
			SessionName: s.Name,
			WindowIndex: "0",
			WindowName:  s.Name,
			PaneIndex:   "0",
			// The pane id is the session name itself: it is the one stable handle
			// there is, and every send/capture below normalises it back.
			PaneID:  s.Name,
			PID:     s.PID,
			Command: s.Command,
			Path:    s.Cwd,
			Active:  true,
		})
	}
	return out, nil
}

// HasSession reports whether a session with the exact name exists.
func (c *Client) HasSession(_, name string) (bool, error) {
	res, err := c.call(request{Op: "has", Name: name})
	if err != nil {
		return false, err
	}
	return res.Exists, nil
}

// NewSession creates a detached session rooted at cwd, running the platform
// shell — exactly the shape tmux new-session leaves behind.
func (c *Client) NewSession(_, name, cwd string) error {
	_, err := c.call(request{Op: "new", Name: name, Cwd: cwd})
	return err
}

// KillSession terminates a session and everything running inside it.
func (c *Client) KillSession(_, name string) error {
	_, err := c.call(request{Op: "kill", Name: name})
	// A session that is already gone is the outcome the caller wanted.
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "no session named") {
		return nil
	}
	return err
}

// RenameSession renames a session in place.
func (c *Client) RenameSession(_, from, to string) error {
	_, err := c.call(request{Op: "rename", Name: from, To: to})
	return err
}

// SendText types literal text into a session, optionally pressing Enter.
func (c *Client) SendText(_, tgt, text string, pressEnter bool) error {
	_, err := c.call(request{
		Op: "send", Name: tgt,
		B64: base64.StdEncoding.EncodeToString([]byte(text)), Enter: pressEnter,
	})
	return err
}

// SendKey sends a named key such as "C-c" or "Escape".
func (c *Client) SendKey(_, tgt, key string) error {
	_, err := c.call(request{Op: "key", Name: tgt, Key: key})
	return err
}

// CapturePane returns the last n lines of a session's scrollback as plain text.
func (c *Client) CapturePane(_, tgt string, lines int) (string, error) {
	res, err := c.call(request{Op: "capture", Name: tgt, Lines: lines})
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(res.B64)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// OpenAttach connects a live terminal to a session, in the shape the shell
// manager adopts: a Session to write and resize, a Reader of merged output.
// Closing it detaches; the session keeps running in the daemon.
func (c *Client) OpenAttach(session string, cols, rows int) (sshx.Opened, error) {
	conn, release, sc, err := c.open()
	if err != nil {
		return sshx.Opened{}, err
	}
	fail := func(err error) (sshx.Opened, error) {
		_ = conn.Close()
		release()
		return sshx.Opened{}, err
	}
	if err := roundTrip(conn, sc, request{Op: "attach", Name: session, Cols: cols, Rows: rows}); err != nil {
		return fail(err)
	}

	at := &attached{conn: conn, done: make(chan struct{})}
	pr, pw := io.Pipe()
	go at.pump(sc, pw)
	return sshx.Opened{Session: at, Stdout: pr, Release: release}, nil
}

// attached is the client half of an attached session terminal.
type attached struct {
	conn io.ReadWriteCloser

	writeMu sync.Mutex
	once    sync.Once
	done    chan struct{}
	reason  string
}

// pump turns daemon frames back into the byte stream a terminal reads.
func (a *attached) pump(sc *bufio.Scanner, pw *io.PipeWriter) {
	defer pw.Close()
	for sc.Scan() {
		var f frame
		if json.Unmarshal(sc.Bytes(), &f) != nil {
			break
		}
		switch f.Stream {
		case "data":
			raw, err := base64.StdEncoding.DecodeString(f.B64)
			if err != nil {
				continue
			}
			if _, err := pw.Write(raw); err != nil {
				return
			}
		case "exit":
			a.finish(f.Reason)
			return
		}
	}
	a.finish("lost the session daemon")
}

func (a *attached) finish(reason string) {
	a.once.Do(func() {
		a.reason = reason
		close(a.done)
	})
}

func (a *attached) send(f frame) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	return json.NewEncoder(a.conn).Encode(f)
}

func (a *attached) Write(p []byte) (int, error) {
	if err := a.send(frame{Stream: "data", B64: base64.StdEncoding.EncodeToString(p)}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (a *attached) Resize(cols, rows int) error {
	return a.send(frame{Stream: "resize", Cols: cols, Rows: rows})
}

// Wait blocks until the attachment ends and says why. A detach initiated by
// Close ends it with no complaint, exactly like closing a tmux client.
func (a *attached) Wait() error {
	<-a.done
	if a.reason == "" || a.reason == "detached" {
		return nil
	}
	return errors.New(a.reason)
}

// Close detaches. The session and everything inside it keeps running.
func (a *attached) Close() error {
	// The ending is recorded before the frame goes out, because the daemon
	// hangs up the moment it reads a detach: the pump would see that hangup as
	// a lost daemon and, being first, would name the ending. A detach the user
	// asked for has to read as a detach — in the terminal's closing line, and
	// to Wait, which is the difference between a quiet return and an error.
	a.finish("detached")
	_ = a.send(frame{Stream: "detach"})
	return a.conn.Close()
}
