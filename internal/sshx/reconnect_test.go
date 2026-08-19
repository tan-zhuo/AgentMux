package sshx

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// fakeSession is one transport's half of a terminal, driven by the test.
type fakeSession struct {
	mu     sync.Mutex
	writes []byte
	cols   int
	rows   int
	ended  chan error
	closed bool
}

func newFake(cols, rows int) *fakeSession {
	return &fakeSession{cols: cols, rows: rows, ended: make(chan error, 1)}
}

func (f *fakeSession) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, p...)
	return len(p), nil
}

func (f *fakeSession) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cols, f.rows = cols, rows
	return nil
}

func (f *fakeSession) Wait() error { return <-f.ended }

func (f *fakeSession) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.closed {
		f.closed = true
		select {
		case f.ended <- nil:
		default:
		}
	}
	return nil
}

func (f *fakeSession) size() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows
}

// drop ends the session the way a severed connection does.
func (f *fakeSession) drop(err error) { f.ended <- err }

// recorder collects the events the manager emits.
type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) emit(name string, _ any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, name)
}

func (r *recorder) count(name string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e == name {
			n++
		}
	}
	return n
}

func opened(sess Session) Opened {
	pr, pw := io.Pipe()
	go func() {
		<-time.After(time.Hour)
		_ = pw.Close()
	}()
	return Opened{Session: sess, Stdout: pr}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A terminal whose transport dies is dialled again, keeps its id, and comes
// back at the size the window has now rather than the one it opened with.
func TestReconnectAfterTransportFailure(t *testing.T) {
	rec := &recorder{}
	m := NewShellManager(nil, rec.emit)

	first := newFake(80, 24)
	var mu sync.Mutex
	reopens := 0
	second := newFake(80, 24)

	info := m.AdoptWith(ShellOptions{ServerID: "s1", Cols: 80, Rows: 24}, opened(first),
		func(o ShellOptions) (Opened, error) {
			mu.Lock()
			reopens++
			mu.Unlock()
			// The new transport comes up at whatever size it was given; the
			// point of the assertion below is that the manager passes on the
			// size the window has now, not the one this terminal opened with.
			if o.Cols <= 0 || o.Rows <= 0 {
				t.Errorf("reopened with a nonsense size %dx%d", o.Cols, o.Rows)
			}
			return opened(second), nil
		})

	if err := m.Resize(info.ID, 120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	first.drop(errors.New("connection lost"))

	waitFor(t, "the reconnect to land", func() bool { return rec.count("term:reconnect:"+info.ID) > 0 })

	mu.Lock()
	got := reopens
	mu.Unlock()
	if got != 1 {
		t.Fatalf("reopened %d times, want 1", got)
	}
	if rec.count("term:exit:"+info.ID) != 0 {
		t.Fatal("a terminal that reconnected should not have reported an exit")
	}
	if cols, rows := second.size(); cols != 120 || rows != 40 {
		t.Fatalf("reconnected at %dx%d, want 120x40", cols, rows)
	}
	// The same id still drives the terminal, now through the new transport.
	if err := m.Write(info.ID, "aGk="); err != nil {
		t.Fatalf("write after reconnect: %v", err)
	}
	live := m.List()
	if len(live) != 1 || live[0].ID != info.ID {
		t.Fatalf("expected the same terminal to still be open, got %+v", live)
	}
}

// A terminal that ended on its own terms is over, and is not dialled again.
func TestNoReconnectOnExit(t *testing.T) {
	rec := &recorder{}
	m := NewShellManager(nil, rec.emit)

	sess := newFake(80, 24)
	reopened := false
	info := m.AdoptWith(ShellOptions{ServerID: "s1", Cols: 80, Rows: 24}, opened(sess),
		func(ShellOptions) (Opened, error) {
			reopened = true
			return opened(newFake(80, 24)), nil
		})

	sess.drop(&ExitError{Code: 0})

	waitFor(t, "the terminal to end", func() bool { return rec.count("term:exit:"+info.ID) == 1 })
	if reopened {
		t.Fatal("a shell that exited was dialled again")
	}
}

// Closing a tab stops a reconnect that is already under way.
func TestCloseStopsReconnect(t *testing.T) {
	rec := &recorder{}
	m := NewShellManager(nil, rec.emit)

	sess := newFake(80, 24)
	var mu sync.Mutex
	reopens := 0
	info := m.AdoptWith(ShellOptions{ServerID: "s1", Cols: 80, Rows: 24}, opened(sess),
		func(ShellOptions) (Opened, error) {
			mu.Lock()
			reopens++
			mu.Unlock()
			return Opened{}, errors.New("still down")
		})

	sess.drop(errors.New("connection lost"))
	// Let the first backoff start, then close the tab.
	time.Sleep(50 * time.Millisecond)
	if err := m.Close(info.ID); err != nil {
		t.Fatalf("close: %v", err)
	}

	time.Sleep(1500 * time.Millisecond)
	mu.Lock()
	got := reopens
	mu.Unlock()
	if got != 0 {
		t.Fatalf("kept dialling a closed terminal (%d attempts)", got)
	}
}
