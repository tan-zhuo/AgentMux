// Package integration exercises the SSH pool, PTY manager and tmux wrapper
// against a real sshd. It is skipped unless AGENTMUX_TEST_HOST is set, so
// `go test ./...` stays offline-safe.
//
// To run it:
//
//	AGENTMUX_TEST_HOST=127.0.0.1 AGENTMUX_TEST_PORT=2222 \
//	AGENTMUX_TEST_USER=you AGENTMUX_TEST_KEY=/path/to/key go test ./internal/integration -v
package integration

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"agentmux/internal/sshx"
	"agentmux/internal/tmuxx"
)

const testSession = "agentmux-itest"

type resolver struct {
	mu       sync.Mutex
	target   sshx.Target
	pinned   string
	markedOK int
}

func (r *resolver) Resolve(string) (sshx.Target, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t := r.target
	t.HostKey = r.pinned
	return t, nil
}

func (r *resolver) PinHostKey(_, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pinned = key
	return nil
}

func (r *resolver) MarkOK(string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markedOK++
}

func (r *resolver) pin() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pinned
}

func newResolver(t *testing.T) *resolver {
	t.Helper()
	host := os.Getenv("AGENTMUX_TEST_HOST")
	if host == "" {
		t.Skip("AGENTMUX_TEST_HOST not set; skipping integration test")
	}
	port, _ := strconv.Atoi(os.Getenv("AGENTMUX_TEST_PORT"))
	if port == 0 {
		port = 22
	}
	return &resolver{target: sshx.Target{
		ServerID: "test-server",
		Name:     "test",
		Host:     host,
		Port:     port,
		User:     os.Getenv("AGENTMUX_TEST_USER"),
		AuthType: "key",
		KeyPath:  os.Getenv("AGENTMUX_TEST_KEY"),
	}}
}

func newPool(t *testing.T) (*sshx.Pool, *resolver) {
	t.Helper()
	res := newResolver(t)
	pool := sshx.NewPool(res, 0, nil)
	t.Cleanup(pool.Stop)
	return pool, res
}

func TestConnectAndProbe(t *testing.T) {
	pool, res := newPool(t)

	probe := pool.TestConnection("test-server")
	if !probe.OK {
		t.Fatalf("connection failed: %s", probe.Error)
	}
	if probe.OS == "" {
		t.Errorf("expected an OS string, got empty")
	}
	if !probe.HasTmux {
		t.Fatalf("tmux is required on the test host, probe reported none")
	}
	t.Logf("connected in %d ms | %s | %s", probe.LatencyMS, probe.OS, probe.TmuxVer)

	if res.pin() == "" {
		t.Error("host key was not pinned on first connection")
	}
}

func TestHostKeyMismatchIsRejected(t *testing.T) {
	pool, res := newPool(t)

	// Warm the pool so the real key gets pinned, then corrupt the pin.
	if p := pool.TestConnection("test-server"); !p.OK {
		t.Fatalf("initial connection failed: %s", p.Error)
	}
	pool.Disconnect("test-server")

	real := res.pin()
	_ = res.PinHostKey("test-server", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIB0000000000000000000000000000000000000000")

	probe := pool.TestConnection("test-server")
	if probe.OK {
		t.Fatal("expected a mismatched host key to abort the connection")
	}
	if !strings.Contains(probe.Error, "does not match the pinned key") {
		t.Errorf("expected a host key mismatch error, got: %s", probe.Error)
	}
	_ = res.PinHostKey("test-server", real)
}

func TestConnectionIsReusedAcrossCalls(t *testing.T) {
	pool, _ := newPool(t)

	first, err := pool.Acquire("test-server")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	second, err := pool.Acquire("test-server")
	if err != nil {
		t.Fatalf("second acquire: %v", err)
	}
	if first.Client != second.Client {
		t.Error("expected both leases to share one SSH connection")
	}
	if got := pool.ActiveRefs("test-server"); got != 2 {
		t.Errorf("expected 2 outstanding leases, got %d", got)
	}
	first.Release()
	second.Release()
	if got := pool.ActiveRefs("test-server"); got != 0 {
		t.Errorf("expected leases to drop to 0, got %d", got)
	}
	if !pool.IsConnected("test-server") {
		t.Error("connection should stay pooled after the last lease is released")
	}
}

func TestTmuxSessionLifecycle(t *testing.T) {
	pool, _ := newPool(t)
	tm := tmuxx.New(pool)

	// Start from a clean slate even if a previous run died halfway.
	_ = tm.KillSession("test-server", testSession)
	t.Cleanup(func() { _ = tm.KillSession("test-server", testSession) })

	if err := tm.NewSession("test-server", testSession, ""); err != nil {
		t.Fatalf("new-session: %v", err)
	}

	ok, err := tm.HasSession("test-server", testSession)
	if err != nil {
		t.Fatalf("has-session: %v", err)
	}
	if !ok {
		t.Fatal("session was created but has-session says it does not exist")
	}

	sessions, err := tm.ListSessions("test-server")
	if err != nil {
		t.Fatalf("list-sessions: %v", err)
	}
	var found bool
	for _, s := range sessions {
		if s.Name == testSession {
			found = true
			if s.Windows < 1 {
				t.Errorf("expected at least one window, got %d", s.Windows)
			}
		}
	}
	if !found {
		t.Fatalf("new session missing from list-sessions: %+v", sessions)
	}

	panes, err := tm.ListPanes("test-server")
	if err != nil {
		t.Fatalf("list-panes: %v", err)
	}
	var pane *tmuxx.Pane
	for i := range panes {
		if panes[i].SessionName == testSession {
			pane = &panes[i]
			break
		}
	}
	if pane == nil {
		t.Fatalf("no panes reported for %s", testSession)
	}
	if pane.PaneID == "" || pane.PID == 0 {
		t.Errorf("expected a usable pane id and pid, got %+v", pane)
	}
	t.Logf("pane %s running %q in %s", pane.PaneID, pane.Command, pane.Path)

	// Literal send: the marker contains words tmux would otherwise interpret as
	// key names, which is exactly what an agent prompt looks like.
	marker := "echo IT-MARKER-Enter-C-c-done"
	if err := tm.SendText("test-server", pane.PaneID, marker, true); err != nil {
		t.Fatalf("send-keys: %v", err)
	}

	var capture string
	for i := 0; i < 25; i++ {
		time.Sleep(200 * time.Millisecond)
		capture, err = tm.CapturePane("test-server", pane.PaneID, 50)
		if err != nil {
			t.Fatalf("capture-pane: %v", err)
		}
		if strings.Contains(capture, "IT-MARKER-Enter-C-c-done") {
			break
		}
	}
	if !strings.Contains(capture, "IT-MARKER-Enter-C-c-done") {
		t.Fatalf("marker never appeared in pane capture:\n%s", capture)
	}

	if err := tm.KillSession("test-server", testSession); err != nil {
		t.Fatalf("kill-session: %v", err)
	}
	ok, err = tm.HasSession("test-server", testSession)
	if err != nil {
		t.Fatalf("has-session after kill: %v", err)
	}
	if ok {
		t.Error("session still exists after kill-session")
	}
}

func TestSessionSurvivesDetach(t *testing.T) {
	pool, _ := newPool(t)
	tm := tmuxx.New(pool)

	_ = tm.KillSession("test-server", testSession)
	t.Cleanup(func() { _ = tm.KillSession("test-server", testSession) })
	if err := tm.NewSession("test-server", testSession, ""); err != nil {
		t.Fatalf("new-session: %v", err)
	}

	var (
		mu  sync.Mutex
		buf strings.Builder
	)
	shells := sshx.NewShellManager(pool, func(name string, data any) {
		d, ok := data.(sshx.TermData)
		if !ok {
			return
		}
		raw, err := base64.StdEncoding.DecodeString(d.Base64)
		if err != nil {
			return
		}
		mu.Lock()
		buf.Write(raw)
		mu.Unlock()
	})

	info, err := shells.Open(sshx.ShellOptions{
		ServerID: "test-server",
		Cols:     100,
		Rows:     30,
		Command:  tmuxx.AttachCommand(testSession),
	})
	if err != nil {
		t.Fatalf("attach pty: %v", err)
	}

	// Type into the attached terminal the way the UI does.
	if err := shells.Write(info.ID, base64.StdEncoding.EncodeToString([]byte("echo PTY-ALIVE\n"))); err != nil {
		t.Fatalf("write: %v", err)
	}

	read := func() string {
		mu.Lock()
		defer mu.Unlock()
		return buf.String()
	}
	var seen bool
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		if strings.Contains(read(), "PTY-ALIVE") {
			seen = true
			break
		}
	}
	if !seen {
		t.Fatalf("PTY output never arrived; got:\n%s", read())
	}

	// Start something long-lived, then detach by closing the terminal.
	if err := shells.Write(info.ID, base64.StdEncoding.EncodeToString([]byte("sleep 120\n"))); err != nil {
		t.Fatalf("write sleep: %v", err)
	}
	time.Sleep(1200 * time.Millisecond)

	if err := shells.Close(info.ID); err != nil {
		t.Fatalf("close pty: %v", err)
	}

	// The point of the whole product: the session is still there afterwards.
	ok, err := tm.HasSession("test-server", testSession)
	if err != nil {
		t.Fatalf("has-session after detach: %v", err)
	}
	if !ok {
		t.Fatal("tmux session died when the terminal was closed")
	}

	panes, err := tm.ListPanes("test-server")
	if err != nil {
		t.Fatalf("list-panes after detach: %v", err)
	}
	var cmd string
	for _, p := range panes {
		if p.SessionName == testSession {
			cmd = p.Command
		}
	}
	if cmd != "sleep" {
		t.Errorf("expected the detached pane to still be running sleep, got %q", cmd)
	}
	t.Logf("after detach the pane is still running %q", cmd)
}

func TestScrollbackReplay(t *testing.T) {
	pool, _ := newPool(t)
	shells := sshx.NewShellManager(pool, func(string, any) {})

	info, err := shells.Open(sshx.ShellOptions{ServerID: "test-server", Cols: 90, Rows: 24})
	if err != nil {
		t.Fatalf("open shell: %v", err)
	}
	t.Cleanup(func() { _ = shells.Close(info.ID) })

	if err := shells.Write(info.ID, base64.StdEncoding.EncodeToString([]byte("echo REPLAY-OK\n"))); err != nil {
		t.Fatalf("write: %v", err)
	}

	var decoded string
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		b64, err := shells.Scrollback(info.ID)
		if err != nil {
			t.Fatalf("scrollback: %v", err)
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			t.Fatalf("decode scrollback: %v", err)
		}
		decoded = string(raw)
		if strings.Contains(decoded, "REPLAY-OK") {
			return
		}
	}
	t.Fatalf("scrollback never captured the output; got:\n%s", decoded)
}
