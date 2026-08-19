package natmux

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tcpTransport is what the production remote transport looks like from this
// package's side: a dial to the daemon's TCP port and a token read as the same
// account — here, a plain file read standing in for the SSH exec.
type tcpTransport struct {
	addr      string
	tokenPath string
	token     string
}

func (t *tcpTransport) Dial() (io.ReadWriteCloser, func(), error) {
	conn, err := net.DialTimeout("tcp", t.addr, 2*time.Second)
	if err != nil {
		return nil, nil, err
	}
	return conn, func() {}, nil
}

func (t *tcpTransport) Ensure() error { return nil }

func (t *tcpTransport) Token() (string, error) {
	if t.token != "" {
		return t.token, nil
	}
	raw, err := os.ReadFile(t.tokenPath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// startTCPDaemon runs the daemon with its TCP listener enabled on a port chosen
// to be free, returning the address and the token file path.
func startTCPDaemon(t *testing.T) (string, string) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	_ = probe.Close()

	t.Setenv("AGENTMUX_NATMUX_SOCKET", filepath.Join(t.TempDir(), "natmux.sock"))
	t.Setenv("AGENTMUX_NATMUX_TCP", addr)
	go DaemonMain()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond); err == nil {
			_ = conn.Close()
			// Asked for rather than assembled: where the token lands is the
			// daemon's business, and the two must not guess at it separately.
			return addr, tokenPath()
		}
		if time.Now().After(deadline) {
			log, _ := os.ReadFile(daemonLogPath())
			t.Fatalf("tcp daemon did not come up on %s\n%s", addr, strings.TrimSpace(string(log)))
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestRemoteTransportLifecycle(t *testing.T) {
	addr, tokenPath := startTCPDaemon(t)
	c := NewRemoteClient(&tcpTransport{addr: addr, tokenPath: tokenPath})

	if info := c.Available(""); !info.Available {
		t.Fatalf("daemon not available over tcp: %s", info.Error)
	}

	const name = "agentmux/test/remote"
	if err := c.NewSession("", name, t.TempDir()); err != nil {
		t.Fatalf("new session over tcp: %v", err)
	}
	defer c.KillSession("", name)

	if err := c.SendText("", name, echoSum("remote-tcp", 6, 36), true); err != nil {
		t.Fatalf("send over tcp: %v", err)
	}
	waitFor(t, "output over tcp", func() bool {
		out, err := c.CapturePane("", name, 100)
		return err == nil && strings.Contains(out, "remote-tcp-42")
	})

	// Attach streams through the same authenticated connection.
	opened, err := c.OpenAttach(name, 100, 30)
	if err != nil {
		t.Fatalf("attach over tcp: %v", err)
	}
	buf := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(opened.Stdout)
		buf <- string(raw)
	}()
	if _, err := opened.Session.Write([]byte("echo attach-tcp-ok\r")); err != nil {
		t.Fatalf("write over tcp attach: %v", err)
	}
	time.Sleep(600 * time.Millisecond)
	_ = opened.Session.Close()
	out := <-buf
	if !strings.Contains(out, "attach-tcp-ok") && !strings.Contains(out, "remote-tcp-42") {
		t.Fatalf("attach stream carried nothing recognisable: %q", out)
	}

	// Detaching left the session alive.
	if ok, err := c.HasSession("", name); err != nil || !ok {
		t.Fatalf("session should survive tcp detach: ok=%v err=%v", ok, err)
	}
}

func TestTCPRequiresToken(t *testing.T) {
	addr, _ := startTCPDaemon(t)

	// No auth at all: the first real request must be refused.
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "%s\n", `{"op":"list"}`)
	reply := make([]byte, 256)
	n, _ := conn.Read(reply)
	if !strings.Contains(string(reply[:n]), "authenticate first") {
		t.Fatalf("unauthenticated request should be refused, got %q", reply[:n])
	}

	// A wrong token must be refused too.
	conn2, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	fmt.Fprintf(conn2, "%s\n", `{"op":"auth","token":"wrong"}`)
	n, _ = conn2.Read(reply)
	if !strings.Contains(string(reply[:n]), "bad token") {
		t.Fatalf("wrong token should be refused, got %q", reply[:n])
	}
}

func TestTCPPortDeterministic(t *testing.T) {
	a, b := TCPPort("Alice"), TCPPort("alice")
	if a != b {
		t.Fatal("port must not depend on username case")
	}
	if a < 47000 || a >= 49000 {
		t.Fatalf("port %d outside the expected range", a)
	}
	if TCPPort("alice") == TCPPort("bob") {
		t.Log("alice and bob collide; acceptable but worth knowing")
	}
}
