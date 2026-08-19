package natmux

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// startTestDaemon runs the daemon in-process on a private socket. The goroutine
// leaks for the remainder of the test binary, which is the cost of testing the
// real accept loop rather than a mock of it.
func startTestDaemon(t *testing.T) *Client {
	t.Helper()
	t.Setenv("AGENTMUX_NATMUX_SOCKET", filepath.Join(t.TempDir(), "natmux.sock"))
	go DaemonMain()

	waitForDaemon(t)
	return NewClient()
}

// waitForDaemon blocks until the daemon answers, and says why it never did
// rather than only that it did not. The daemon's one report of a failed listen
// goes to its log file, so a bare timeout here costs a round trip through CI to
// learn something the machine already knew.
func waitForDaemon(t *testing.T) {
	t.Helper()
	c := NewClient()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if info := c.Available(""); info.Available {
			return
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(daemonLogPath())
			t.Fatalf("daemon did not come up on %s\n%s", addrLabel(), bytes.TrimSpace(log))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// echoSum is a command that proves the session's shell ran what it was sent
// rather than echoing it back — written in the arithmetic of whichever shell
// this platform starts. The POSIX form is a parse error in PowerShell, which is
// how a test that reads as portable turns out not to be.
func echoSum(marker string, a, b int) string {
	if runtime.GOOS == "windows" {
		return fmt.Sprintf(`Write-Output "%s-$(%d+%d)"`, marker, a, b)
	}
	return fmt.Sprintf("echo %s-$((%d+%d))", marker, a, b)
}

// waitFor polls until check passes or the deadline hits.
func waitFor(t *testing.T, what string, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if check() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func TestSessionLifecycle(t *testing.T) {
	c := startTestDaemon(t)
	dir := t.TempDir()

	const name = "agentmux/test/lifecycle"
	if err := c.NewSession("", name, dir); err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := c.NewSession("", name, dir); err == nil {
		t.Fatal("creating a duplicate session should refuse")
	}

	ok, err := c.HasSession("", name)
	if err != nil || !ok {
		t.Fatalf("has session: ok=%v err=%v", ok, err)
	}
	// The exact-match spelling the tmux paths use must address the same session.
	ok, err = c.HasSession("", "="+name)
	if err != nil || !ok {
		t.Fatalf("has =session: ok=%v err=%v", ok, err)
	}

	panes, err := c.ListPanes("")
	if err != nil || len(panes) != 1 {
		t.Fatalf("list panes: %v %v", panes, err)
	}
	if panes[0].SessionName != name || panes[0].PID <= 0 {
		t.Fatalf("pane looks wrong: %+v", panes[0])
	}
	if panes[0].Path != dir {
		t.Fatalf("pane path = %q, want %q", panes[0].Path, dir)
	}

	// Type a command and read it back from the scrollback capture.
	if err := c.SendText("", name, echoSum("natmux-says", 40, 2), true); err != nil {
		t.Fatalf("send text: %v", err)
	}
	waitFor(t, "command output in capture", func() bool {
		out, err := c.CapturePane("", name, 100)
		return err == nil && strings.Contains(out, "natmux-says-42")
	})

	// Rename, then address it by the new name.
	const renamed = "agentmux/test/renamed"
	if err := c.RenameSession("", name, renamed); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if ok, _ := c.HasSession("", name); ok {
		t.Fatal("old name still answers after rename")
	}
	if ok, _ := c.HasSession("", renamed); !ok {
		t.Fatal("new name does not answer after rename")
	}

	if err := c.KillSession("", renamed); err != nil {
		t.Fatalf("kill: %v", err)
	}
	waitFor(t, "session to die", func() bool {
		ok, err := c.HasSession("", renamed)
		return err == nil && !ok
	})
	// Killing a dead session is the outcome the caller wanted, not an error.
	if err := c.KillSession("", renamed); err != nil {
		t.Fatalf("kill of a dead session: %v", err)
	}
}

func TestAttachDetachAndPersistence(t *testing.T) {
	c := startTestDaemon(t)

	const name = "agentmux/test/attach"
	if err := c.NewSession("", name, t.TempDir()); err != nil {
		t.Fatalf("new session: %v", err)
	}
	defer c.KillSession("", name)

	// Seed the scrollback before attaching, to prove backlog replay.
	if err := c.SendText("", name, "echo before-attach-marker", true); err != nil {
		t.Fatalf("send: %v", err)
	}
	waitFor(t, "marker in capture", func() bool {
		out, _ := c.CapturePane("", name, 100)
		return strings.Contains(out, "before-attach-marker")
	})

	opened, err := c.OpenAttach(name, 100, 30)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}

	var (
		outMu  sync.Mutex
		outBuf bytes.Buffer
	)
	seen := func(marker string) bool {
		outMu.Lock()
		defer outMu.Unlock()
		return strings.Contains(outBuf.String(), marker)
	}
	go func() {
		buf := make([]byte, 8192)
		for {
			n, err := opened.Stdout.Read(buf)
			if n > 0 {
				outMu.Lock()
				outBuf.Write(buf[:n])
				outMu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	// The backlog must arrive without typing anything.
	waitFor(t, "backlog replay", func() bool {
		return seen("before-attach-marker")
	})

	// Live input through the attachment.
	if _, err := opened.Session.Write([]byte("echo live-attach-marker\r")); err != nil {
		t.Fatalf("write: %v", err)
	}
	waitFor(t, "live output", func() bool {
		return seen("live-attach-marker")
	})
	if err := opened.Session.Resize(80, 24); err != nil {
		t.Fatalf("resize: %v", err)
	}

	// Detaching leaves the session running: that is the whole promise.
	if err := opened.Session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if ok, err := c.HasSession("", name); err != nil || !ok {
		t.Fatalf("session should survive detach: ok=%v err=%v", ok, err)
	}
	if err := opened.Session.Wait(); err != nil {
		t.Fatalf("wait after detach should be quiet, got %v", err)
	}

	// And it is still usable afterwards.
	if err := c.SendText("", name, "echo after-detach-marker", true); err != nil {
		t.Fatalf("send after detach: %v", err)
	}
	waitFor(t, "output after detach", func() bool {
		out, _ := c.CapturePane("", name, 100)
		return strings.Contains(out, "after-detach-marker")
	})
}

func TestShellExitEndsSession(t *testing.T) {
	c := startTestDaemon(t)

	const name = "agentmux/test/exit"
	if err := c.NewSession("", name, t.TempDir()); err != nil {
		t.Fatalf("new session: %v", err)
	}
	opened, err := c.OpenAttach(name, 80, 24)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, opened.Stdout) }()

	if err := c.SendText("", name, "exit", true); err != nil {
		t.Fatalf("send exit: %v", err)
	}
	waitFor(t, "session to end", func() bool {
		ok, err := c.HasSession("", name)
		return err == nil && !ok
	})
	// The attached terminal is told, and Wait phrases it as an ending.
	done := make(chan error, 1)
	go func() { done <- opened.Session.Wait() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("attached terminal never learned the session ended")
	}
}

func TestStripANSI(t *testing.T) {
	in := "\x1b[31mred\x1b[0m plain \x1b]0;title\x07after"
	if got := stripANSI(in); got != "red plain after" {
		t.Fatalf("stripANSI = %q", got)
	}
}

func TestLastLines(t *testing.T) {
	if got := lastLines("a\nb\nc\nd", 2); got != "c\nd" {
		t.Fatalf("lastLines = %q", got)
	}
	// A progress bar's carriage returns resolve to the final state.
	if got := lastLines("10%\r50%\r100%", 5); got != "100%" {
		t.Fatalf("lastLines with CR = %q", got)
	}
}
