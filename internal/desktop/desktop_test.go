package desktop

import (
	"encoding/json"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// hostDialer stands in for the SSH connection: dialing "through" it reaches
// this machine, which is where the tests put their pretend desktops.
type hostDialer struct {
	mu    sync.Mutex
	calls []string
	// ports maps the port a caller asks the host for to one that is really
	// listening here, so a test can pretend something answers on 3389.
	ports map[int]string
}

func (h *hostDialer) Dial(network, address string) (net.Conn, error) {
	h.mu.Lock()
	h.calls = append(h.calls, address)
	_, port, _ := net.SplitHostPort(address)
	real, ok := h.ports[atoi(port)]
	h.mu.Unlock()
	if !ok {
		// Nothing is listening there. Refused rather than hung, which is the
		// friendly half of what a real host does.
		return nil, &net.OpError{Op: "dial", Net: network, Err: errRefused{}}
	}
	return net.Dial("tcp", real)
}

type errRefused struct{}

func (errRefused) Error() string { return "connection refused" }

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// echoServer answers with whatever it is sent, which is enough to prove bytes
// went all the way through a forward and back.
func echoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	return ln.Addr().String()
}

func TestProbeFindsWhatIsListeningAndNothingElse(t *testing.T) {
	host := &hostDialer{ports: map[int]string{5900: echoServer(t)}}

	found := Probe(host, 2*time.Second)
	if len(found) != 1 || found[0].Protocol != VNC || found[0].Port != 5900 {
		t.Fatalf("expected the one live port, got %+v", found)
	}
	// Every usual port is asked about, and no others: a tool that scans
	// somebody's machine on its own is not what this is.
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.calls) != len(Usual) {
		t.Fatalf("expected %d dials, got %v", len(Usual), host.calls)
	}
	for _, addr := range host.calls {
		if !strings.HasPrefix(addr, "127.0.0.1:") {
			t.Fatalf("a probe left the host's loopback: %s", addr)
		}
	}
}

func TestProbeReportsBothWhenBothAnswer(t *testing.T) {
	host := &hostDialer{ports: map[int]string{3389: echoServer(t), 5901: echoServer(t)}}
	found := Probe(host, 2*time.Second)
	if len(found) != 2 || found[0].Protocol != RDP || found[1].Port != 5901 {
		t.Fatalf("expected rdp then vnc in the listed order, got %+v", found)
	}
}

func TestProbeDoesNotWaitOnAHostThatNeverAnswers(t *testing.T) {
	// A dial that hangs is the case the timeout exists for: an SSH channel to a
	// filtered port sits until the far side's stack gives up.
	host := &hangingDialer{}
	start := time.Now()
	if found := Probe(host, 150*time.Millisecond); len(found) != 0 {
		t.Fatalf("nothing was listening, got %+v", found)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("probing took %s, so the dials were not bounded", elapsed)
	}
}

type hangingDialer struct{}

func (hangingDialer) Dial(string, string) (net.Conn, error) {
	select {}
}

func TestAForwardCarriesBytesToTheHostAndBack(t *testing.T) {
	target := echoServer(t)
	host := &hostDialer{ports: map[int]string{5900: target}}

	closed := make(chan struct{})
	f, err := Open(host, "127.0.0.1:5900", func() { close(closed) })
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()

	if !strings.HasPrefix(f.Local, "127.0.0.1:") {
		t.Fatalf("the door was opened somewhere other than loopback: %s", f.Local)
	}

	conn, err := net.Dial("tcp", f.Local)
	if err != nil {
		t.Fatalf("dial the forward: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("hello desktop\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 64)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got := string(buf[:n]); got != "hello desktop\n" {
		t.Fatalf("the far end said %q", got)
	}

	// Closing releases whatever the dialer came from — in production, the SSH
	// lease that was holding the connection open.
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("closing a forward did not release what it was holding")
	}
	if !f.Closed() {
		t.Fatal("a closed forward does not say so")
	}
	if _, err := net.DialTimeout("tcp", f.Local, 200*time.Millisecond); err == nil {
		t.Fatal("the door is still open after being closed")
	}
}

func TestEndpointsRoundTripThroughTheirStoredForm(t *testing.T) {
	for _, want := range []Endpoint{{Protocol: RDP, Port: 3389}, {Protocol: VNC, Port: 5901}} {
		got, ok := ParseEndpoint(want.String())
		if !ok || got != want {
			t.Fatalf("%s came back as %+v (ok=%v)", want, got, ok)
		}
	}
	for _, bad := range []string{"", "rdp", "rdp:", "ftp:21", "vnc:0", "vnc:99999", "vnc:noport"} {
		if _, ok := ParseEndpoint(bad); ok {
			t.Fatalf("%q was accepted as an endpoint", bad)
		}
	}
}

func TestEveryPlatformKnowsAClientForBothProtocols(t *testing.T) {
	// The commands cannot be run here, but which ones would be tried is a
	// decision worth holding: a protocol with no candidates at all is a feature
	// that silently does nothing on somebody's operating system.
	for _, p := range []Protocol{RDP, VNC} {
		if len(clients(p, "127.0.0.1:1")) == 0 {
			t.Fatalf("no %s client is offered on this platform", p)
		}
	}
}

// Nothing listening is the ordinary answer on a server, and it has to survive
// the trip to the frontend as an empty list rather than as null.
func TestProbeAnswersWithAListEvenWhenEmpty(t *testing.T) {
	found := Probe(&hostDialer{}, 200*time.Millisecond)
	if found == nil {
		t.Fatal("Probe returned a nil slice, which marshals to null")
	}
	if len(found) != 0 {
		t.Fatalf("nothing is listening, got %+v", found)
	}
	b, err := json.Marshal(struct {
		Found []Endpoint `json:"found"`
	}{found})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"found":[]}` {
		t.Errorf("frontend would receive %s", b)
	}
}
