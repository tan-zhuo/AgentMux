package natmux

import (
	"bufio"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// loadOrCreateToken reads the shared secret TCP clients must present, minting
// it on first use. Possessing it proves the caller can read this user's files,
// which is the exact trust the pipe's ACL encodes.
func loadOrCreateToken(path string) (string, error) {
	if raw, err := os.ReadFile(path); err == nil {
		if tok := strings.TrimSpace(string(raw)); tok != "" {
			return tok, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// maxLine bounds one protocol line. Data frames carry at most 32 KiB of raw
// bytes, which is ~43 KiB in base64; a megabyte leaves room to spare.
const maxLine = 1 << 20

// Daemon owns every native session on this machine.
type Daemon struct {
	mu         sync.Mutex
	sessions   map[string]*session
	conns      int
	hadSession bool
	started    time.Time
}

// DaemonMain is the entry point for the --natmuxd process. It never returns
// except to end the process.
func DaemonMain() {
	logTo(daemonLogPath())
	d := &Daemon{sessions: map[string]*session{}, started: time.Now()}
	ln, err := listen()
	if err != nil {
		// Most likely another daemon already holds the address, which means the
		// job is done: there is one broker, and it is not this process.
		log.Printf("natmux: not listening: %v", err)
		return
	}
	defer ln.Close()
	log.Printf("natmux: serving on %s", addrLabel())

	listeners := []net.Listener{ln}
	// The loopback TCP listener is what an SSH client's port forward reaches:
	// it is how a remote AgentMux drives this machine's sessions without a shell
	// in between. It requires the token, because unlike the pipe, localhost TCP
	// has no access control of its own.
	if tln, token, err := listenTCP(); err != nil {
		log.Printf("natmux: no tcp listener: %v", err)
	} else if tln != nil {
		log.Printf("natmux: serving on %s (token required)", tln.Addr())
		listeners = append(listeners, tln)
		defer tln.Close()
		go d.acceptLoop(tln, token)
	}

	go d.idleExit(listeners)
	d.acceptLoop(ln, "")
}

// acceptLoop serves one listener until it closes. A non-empty token means every
// connection must authenticate before its first real request.
func (d *Daemon) acceptLoop(ln net.Listener, token string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		go d.serve(conn, token)
	}
}

// idleExit ends the daemon once it has nothing to keep alive: no sessions and
// no clients. A daemon whose sessions all ended has kept its promise; one that
// never received a session was started for nothing.
func (d *Daemon) idleExit(listeners []net.Listener) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		d.mu.Lock()
		idle := len(d.sessions) == 0 && d.conns == 0
		done := idle && (d.hadSession || time.Since(d.started) > 15*time.Minute)
		d.mu.Unlock()
		if done {
			log.Printf("natmux: idle, exiting")
			for _, ln := range listeners {
				_ = ln.Close()
			}
			os.Exit(0)
		}
	}
}

func (d *Daemon) serve(conn net.Conn, token string) {
	d.mu.Lock()
	d.conns++
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		d.conns--
		d.mu.Unlock()
		_ = conn.Close()
	}()

	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), maxLine)
	enc := json.NewEncoder(conn)

	authed := token == ""
	for sc.Scan() {
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = enc.Encode(response{Error: "bad request: " + err.Error()})
			return
		}
		if req.Op == "auth" {
			// Compared in constant time: the token is what stands between another
			// local account and typing into these sessions.
			if !authed && subtle.ConstantTimeCompare([]byte(req.Token), []byte(token)) == 1 {
				authed = true
				_ = enc.Encode(response{OK: true})
				continue
			}
			if authed {
				_ = enc.Encode(response{OK: true})
				continue
			}
			_ = enc.Encode(response{Error: "bad token"})
			return
		}
		if !authed {
			_ = enc.Encode(response{Error: "authenticate first"})
			return
		}
		if req.Op == "attach" {
			// attach consumes the rest of the connection.
			d.attach(conn, sc, enc, req)
			return
		}
		if err := enc.Encode(d.handle(req)); err != nil {
			return
		}
	}
}

func (d *Daemon) handle(req request) response {
	switch req.Op {
	case "ping":
		return response{OK: true, Version: Version}
	case "list":
		return response{OK: true, Sessions: d.list()}
	case "has":
		_, err := d.get(req.Name)
		return response{OK: true, Exists: err == nil}
	case "new":
		return errResp(d.newSession(req.Name, req.Cwd, req.Cols, req.Rows))
	case "kill":
		s, err := d.get(req.Name)
		if err != nil {
			return errResp(err)
		}
		s.kill()
		return response{OK: true}
	case "rename":
		return errResp(d.rename(req.Name, req.To))
	case "send":
		s, err := d.get(req.Name)
		if err != nil {
			return errResp(err)
		}
		raw, err := base64.StdEncoding.DecodeString(req.B64)
		if err != nil {
			return errResp(fmt.Errorf("bad payload: %w", err))
		}
		if err := s.write(raw); err != nil {
			return errResp(err)
		}
		if req.Enter {
			// A breath between the text and the newline, because several agent
			// TUIs treat paste-then-instant-enter as one gesture to swallow.
			time.Sleep(30 * time.Millisecond)
			if err := s.write([]byte("\r")); err != nil {
				return errResp(err)
			}
		}
		return response{OK: true}
	case "key":
		s, err := d.get(req.Name)
		if err != nil {
			return errResp(err)
		}
		seq, ok := keyBytes(req.Key)
		if !ok {
			return errResp(fmt.Errorf("unknown key %q", req.Key))
		}
		return errResp(s.write(seq))
	case "capture":
		s, err := d.get(req.Name)
		if err != nil {
			return errResp(err)
		}
		return response{OK: true, B64: base64.StdEncoding.EncodeToString([]byte(s.capture(req.Lines)))}
	default:
		return response{Error: fmt.Sprintf("unknown op %q", req.Op)}
	}
}

// attach streams a session over the rest of the connection: backlog first, then
// live output down and keystrokes up, until either side lets go.
func (d *Daemon) attach(conn net.Conn, sc *bufio.Scanner, enc *json.Encoder, req request) {
	s, err := d.get(req.Name)
	if err != nil {
		_ = enc.Encode(errResp(err))
		return
	}
	if req.Cols > 0 && req.Rows > 0 {
		_ = s.pty.Resize(req.Cols, req.Rows)
	}
	id, ch, backlog, ok := s.watch()
	if !ok {
		_ = enc.Encode(response{Error: "session has ended"})
		return
	}
	defer s.unwatch(id)

	if err := enc.Encode(response{OK: true}); err != nil {
		return
	}

	// Writes to the connection come from two directions — the backlog/live pump
	// and nothing else; the read loop below never writes — so the pump goroutine
	// is the sole writer once the ack above is out.
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		if len(backlog) > 0 {
			if enc.Encode(frame{Stream: "data", B64: base64.StdEncoding.EncodeToString(backlog)}) != nil {
				return
			}
		}
		for chunk := range ch {
			if enc.Encode(frame{Stream: "data", B64: base64.StdEncoding.EncodeToString(chunk)}) != nil {
				return
			}
		}
		s.mu.Lock()
		reason := s.reason
		s.mu.Unlock()
		if reason == "" {
			reason = "session ended"
		}
		_ = enc.Encode(frame{Stream: "exit", Reason: reason})
	}()

	for sc.Scan() {
		var f frame
		if json.Unmarshal(sc.Bytes(), &f) != nil {
			break
		}
		switch f.Stream {
		case "data":
			if raw, err := base64.StdEncoding.DecodeString(f.B64); err == nil {
				_ = s.write(raw)
			}
		case "resize":
			if f.Cols > 0 && f.Rows > 0 {
				_ = s.pty.Resize(f.Cols, f.Rows)
			}
		case "detach":
			return
		}
	}
	// The client vanished mid-stream. That is a detach, not an error: closing a
	// tmux client is exactly this.
}

func (d *Daemon) list() []SessionInfo {
	d.mu.Lock()
	sessions := make([]*session, 0, len(d.sessions))
	for _, s := range d.sessions {
		sessions = append(sessions, s)
	}
	d.mu.Unlock()
	out := make([]SessionInfo, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, s.info())
	}
	return out
}

func (d *Daemon) get(name string) (*session, error) {
	name = NormalizeTarget(name)
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.sessions[name]
	if !ok {
		return nil, fmt.Errorf("no session named %q", name)
	}
	return s, nil
}

func (d *Daemon) newSession(name, cwd string, cols, rows int) error {
	name = NormalizeTarget(name)
	if strings.TrimSpace(name) == "" {
		return errors.New("a session needs a name")
	}
	d.mu.Lock()
	if _, exists := d.sessions[name]; exists {
		d.mu.Unlock()
		return fmt.Errorf("a session named %q already exists", name)
	}
	// Reserve the name before the slow part so two racing creates cannot both
	// win; the placeholder is replaced or removed below.
	d.sessions[name] = nil
	d.hadSession = true
	d.mu.Unlock()

	s, err := startSession(name, cwd, cols, rows, d.remove)
	d.mu.Lock()
	if err != nil {
		delete(d.sessions, name)
	} else {
		d.sessions[name] = s
	}
	d.mu.Unlock()
	return err
}

func (d *Daemon) rename(from, to string) error {
	from, to = NormalizeTarget(from), NormalizeTarget(to)
	if strings.TrimSpace(to) == "" {
		return errors.New("a session needs a name")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.sessions[from]
	if !ok || s == nil {
		return fmt.Errorf("no session named %q", from)
	}
	if _, exists := d.sessions[to]; exists {
		return fmt.Errorf("a session named %q already exists", to)
	}
	delete(d.sessions, from)
	d.sessions[to] = s
	s.mu.Lock()
	s.name = to
	s.mu.Unlock()
	return nil
}

// remove drops a session whose shell has ended.
func (d *Daemon) remove(s *session) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s.mu.Lock()
	name := s.name
	s.mu.Unlock()
	if d.sessions[name] == s {
		delete(d.sessions, name)
	}
}

func errResp(err error) response {
	if err != nil {
		return response{Error: err.Error()}
	}
	return response{OK: true}
}

// NormalizeTarget maps the target spellings the application uses for tmux —
// an exact-match "=name", or a pane target — onto the plain session name this
// daemon keys by. Session names here never contain ':', so anything after one
// is tmux window/pane addressing with no meaning locally.
func NormalizeTarget(t string) string {
	t = strings.TrimPrefix(t, "=")
	if i := strings.IndexByte(t, ':'); i >= 0 {
		t = t[:i]
	}
	return t
}

// keyBytes turns the named keys the application actually sends into the bytes a
// terminal would produce for them.
func keyBytes(key string) ([]byte, bool) {
	switch strings.ToLower(key) {
	case "enter":
		return []byte("\r"), true
	case "escape":
		return []byte{0x1b}, true
	case "tab":
		return []byte("\t"), true
	case "space":
		return []byte(" "), true
	case "up":
		return []byte("\x1b[A"), true
	case "down":
		return []byte("\x1b[B"), true
	case "right":
		return []byte("\x1b[C"), true
	case "left":
		return []byte("\x1b[D"), true
	}
	// C-x control chords: the byte is the letter's position in the alphabet.
	k := strings.ToLower(key)
	if strings.HasPrefix(k, "c-") && len(k) == 3 && k[2] >= 'a' && k[2] <= 'z' {
		return []byte{k[2] - 'a' + 1}, true
	}
	return nil, false
}

func logTo(path string) {
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		log.SetOutput(f)
	}
}
