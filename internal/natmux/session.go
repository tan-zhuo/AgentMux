package natmux

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
)

// scrollbackBytes is how much raw output a session keeps for capture and for
// replay into a freshly attached terminal. Matches the shell manager's buffer,
// because they serve the same purpose at different lifetimes.
const scrollbackBytes = 512 * 1024

// session is one persistent terminal the daemon owns: a PTY, the shell inside
// it, its scrollback, and whoever is currently watching.
type session struct {
	name    string
	cwd     string
	created int64
	pty     pty.Pty
	pid     int

	// closeOnce guards the PTY teardown: the reaper closes it when the shell
	// exits and kill closes it deliberately, and the second of those to arrive
	// must be a no-op rather than a race inside the PTY library.
	closeOnce sync.Once

	mu       sync.Mutex
	buf      []byte
	activity int64
	watchers map[int64]chan []byte
	nextID   int64
	dead     bool
	reason   string
}

// startSession creates the PTY, launches the platform shell in it and begins
// pumping output into the scrollback.
func startSession(name, cwd string, cols, rows int, onExit func(*session)) (*session, error) {
	p, err := pty.New()
	if err != nil {
		return nil, fmt.Errorf("open a terminal: %w", err)
	}
	if cols <= 0 {
		cols = 120
	}
	if rows <= 0 {
		rows = 32
	}
	_ = p.Resize(cols, rows)

	shell, args := sessionShell()
	cmd := p.Command(shell, args...)
	cmd.Dir = hostDir(cwd)
	cmd.Env = sessionEnv()
	if err := cmd.Start(); err != nil {
		_ = p.Close()
		return nil, fmt.Errorf("start %s: %w", shell, err)
	}

	now := time.Now().Unix()
	s := &session{
		name:     name,
		cwd:      cwd,
		created:  now,
		activity: now,
		pty:      p,
		watchers: map[int64]chan []byte{},
	}
	if cmd.Process != nil {
		s.pid = cmd.Process.Pid
	}

	go s.pump(onExit)
	// The reaper does two jobs: without the Wait every exited shell stays a
	// zombie, and without the close the pump blocks forever — this process holds
	// its own handle on the terminal's far end, so the shell exiting does not by
	// itself end the read.
	go func() {
		_ = cmd.Wait()
		// A breath for the last output to cross the terminal before it closes.
		time.Sleep(80 * time.Millisecond)
		s.closePTY()
	}()
	return s, nil
}

// closePTY tears down the terminal exactly once, from whichever direction the
// end came.
func (s *session) closePTY() {
	s.closeOnce.Do(func() { _ = s.pty.Close() })
}

// pump copies PTY output into the scrollback and fans it out to watchers. When
// the read side ends — the shell exited, or the session was killed — every
// watcher is told and the daemon drops the session.
func (s *session) pump(onExit func(*session)) {
	buf := make([]byte, 32*1024)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			s.mu.Lock()
			s.appendLocked(chunk)
			s.activity = time.Now().Unix()
			for id, ch := range s.watchers {
				select {
				case ch <- chunk:
				default:
					// A watcher that cannot keep up loses this chunk rather than
					// stalling the session. Interactive programs redraw; the next
					// frame heals the screen.
					_ = id
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			s.mu.Lock()
			if s.reason == "" {
				s.reason = "session ended"
			}
			s.dead = true
			for _, ch := range s.watchers {
				close(ch)
			}
			s.watchers = map[int64]chan []byte{}
			s.mu.Unlock()
			s.closePTY()
			onExit(s)
			return
		}
	}
}

func (s *session) appendLocked(chunk []byte) {
	s.buf = append(s.buf, chunk...)
	if len(s.buf) > scrollbackBytes {
		excess := len(s.buf) - scrollbackBytes
		s.buf = append(s.buf[:0], s.buf[excess:]...)
	}
}

// watch registers a new attached client and returns its stream plus the
// scrollback accumulated so far, in one consistent snapshot.
func (s *session) watch() (id int64, ch chan []byte, backlog []byte, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dead {
		return 0, nil, nil, false
	}
	s.nextID++
	id = s.nextID
	ch = make(chan []byte, 4096)
	s.watchers[id] = ch
	backlog = make([]byte, len(s.buf))
	copy(backlog, s.buf)
	return id, ch, backlog, true
}

// unwatch removes an attached client.
func (s *session) unwatch(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ch, ok := s.watchers[id]; ok {
		delete(s.watchers, id)
		close(ch)
	}
}

// write types bytes into the session's terminal.
func (s *session) write(p []byte) error {
	_, err := s.pty.Write(p)
	return err
}

// capture returns the last n lines of scrollback as plain text: ANSI control
// sequences stripped, carriage-return overwrites resolved. It answers what tmux
// capture-pane answers — what does the pane say — for pollers and log panels.
func (s *session) capture(n int) string {
	s.mu.Lock()
	raw := make([]byte, len(s.buf))
	copy(raw, s.buf)
	s.mu.Unlock()
	return lastLines(stripANSI(string(raw)), n)
}

// info snapshots the session for a listing. The foreground command is resolved
// here, per request, rather than tracked continuously.
func (s *session) info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionInfo{
		Name:     s.name,
		Cwd:      s.cwd,
		Created:  s.created,
		Activity: s.activity,
		Attached: len(s.watchers),
		PID:      s.pid,
		Command:  foregroundCommand(s.pid),
	}
}

// kill ends the session deliberately: the shell and everything under it.
func (s *session) kill() {
	s.mu.Lock()
	if s.reason == "" {
		s.reason = "session killed"
	}
	s.mu.Unlock()
	killTree(s.pid)
	// Closing the PTY unblocks pump's read, which finishes the teardown.
	s.closePTY()
}

// stripANSI removes escape sequences well enough for a status line: CSI and OSC
// sequences go, other escapes drop their introducer.
func stripANSI(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c != 0x1b {
			if c == 0x07 || c == 0x08 {
				i++
				continue
			}
			b.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '[': // CSI: parameters then one final byte in @-~
			i++
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			if i < len(s) {
				i++
			}
		case ']': // OSC: runs to BEL or ST
			i++
			for i < len(s) {
				if s[i] == 0x07 {
					i++
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		default:
			i++
		}
	}
	return b.String()
}

// lastLines keeps the trailing n lines, resolving carriage-return overwrites so
// a progress bar reads as its final state rather than its whole history.
func lastLines(s string, n int) string {
	if n <= 0 {
		n = 200
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	for i, l := range lines {
		if idx := strings.LastIndexByte(l, '\r'); idx >= 0 {
			lines[i] = l[idx+1:]
		}
	}
	return strings.Join(lines, "\n")
}
