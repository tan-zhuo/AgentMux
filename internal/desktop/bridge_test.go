package desktop

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

// pipeSocket is a Socket wired to another one, the way a WebSocket is wired to
// the page at the other end.
type pipeSocket struct {
	in  chan []byte
	out chan []byte
}

func socketPair() (*pipeSocket, *pipeSocket) {
	a, b := make(chan []byte, 64), make(chan []byte, 64)
	return &pipeSocket{in: a, out: b}, &pipeSocket{in: b, out: a}
}

func (s *pipeSocket) Read(ctx context.Context) ([]byte, error) {
	select {
	case msg, ok := <-s.in:
		if !ok {
			return nil, io.EOF
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *pipeSocket) Write(ctx context.Context, b []byte) error {
	msg := append([]byte(nil), b...)
	select {
	case s.out <- msg:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// readWithin fails the test rather than hanging the suite when a message that
// should be on its way never arrives.
func (s *pipeSocket) readWithin(t *testing.T, d time.Duration) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	msg, err := s.Read(ctx)
	if err != nil {
		t.Fatalf("nothing came back: %v", err)
	}
	return msg
}

// VNC asks for nothing but a relay, in both directions.
func TestBridgeRelaysVNC(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	served := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		served <- c
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	client, page := socketPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Bridge(ctx, client, direct{}, Endpoint{Protocol: VNC, Port: port}, nil)
	}()

	conn := <-served
	defer conn.Close()

	// Page to host.
	if err := page.Write(ctx, []byte("RFB 003.008\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 12)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("the host never saw the client's greeting: %v", err)
	}
	if string(buf) != "RFB 003.008\n" {
		t.Errorf("host received %q", buf)
	}

	// Host to page.
	if _, err := conn.Write([]byte("RFB 003.003\n")); err != nil {
		t.Fatal(err)
	}
	if got := string(page.readWithin(t, 3*time.Second)); got != "RFB 003.003\n" {
		t.Errorf("page received %q", got)
	}

	conn.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("bridge ended with %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("the bridge did not notice the host hanging up")
	}
}

// The RDP exchange in full: the client's X.224 request reaches the server, the
// confirm and the certificate chain come back, and the session that follows is
// carried inside the TLS connection this end opened.
func TestBridgeRDPCleanPath(t *testing.T) {
	cert := selfSigned(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	request := tpkt([]byte{0x0e, 0xe0, 0, 0, 0, 0, 0x01}) // an X.224 connection request
	confirm := connectionConfirm(1)                       // the server selects TLS
	sawRequest := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		got := make([]byte, len(request))
		if _, err := io.ReadFull(conn, got); err != nil {
			return
		}
		sawRequest <- got
		if _, err := conn.Write(confirm); err != nil {
			return
		}
		// From here the server is TLS, as an RDP server is after its X.224
		// confirm.
		srv := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
		if err := srv.Handshake(); err != nil {
			return
		}
		buf := make([]byte, 64)
		n, err := srv.Read(buf)
		if err != nil {
			return
		}
		_, _ = srv.Write(append([]byte("echo:"), buf[:n]...))
		// Hold the connection while the test reads the answer.
		time.Sleep(2 * time.Second)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	client, page := socketPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Bridge(ctx, client, direct{}, Endpoint{Protocol: RDP, Port: port}, nil) }()

	// What IronRDP's web client opens with.
	req, err := asn1.Marshal(rdCleanPathPDU{
		Version:     rdCleanPathVersion,
		Destination: "does-not-matter:3389",
		ProxyAuth:   "token",
		X224:        request,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Write(ctx, req); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-sawRequest:
		if string(got) != string(request) {
			t.Errorf("the server received %x", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the X.224 request never reached the server")
	}

	var res rdCleanPathPDU
	if _, err := asn1.Unmarshal(page.readWithin(t, 5*time.Second), &res); err != nil {
		t.Fatalf("the response is not an RDCleanPath PDU: %v", err)
	}
	if res.Version != rdCleanPathVersion {
		t.Errorf("version = %d", res.Version)
	}
	if res.Error.ErrorCode != 0 {
		t.Fatalf("the proxy reported error %d", res.Error.ErrorCode)
	}
	if string(res.X224) != string(confirm) {
		t.Errorf("x224 confirm = %x, want %x", res.X224, confirm)
	}
	if len(res.ServerCertChain) != 1 || string(res.ServerCertChain[0]) != string(cert.Certificate[0]) {
		t.Errorf("the client was not given the server's certificate chain (%d certs)", len(res.ServerCertChain))
	}
	if res.ServerAddr == "" {
		t.Error("the response names no server address")
	}

	// And the session itself, which now travels inside that TLS connection.
	if err := page.Write(ctx, []byte("rdp-bytes")); err != nil {
		t.Fatal(err)
	}
	if got := string(page.readWithin(t, 5*time.Second)); got != "echo:rdp-bytes" {
		t.Errorf("session relay returned %q", got)
	}
}

// A server that selects plain RDP security gets no TLS handshake started at
// it. Old boxes and an xrdp without a certificate both answer this way, and
// starting a handshake anyway is a failure invented by this end.
func TestBridgeRDPHonoursPlainRDPSecurity(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	request := tpkt([]byte{0x0e, 0xe0, 0, 0, 0, 0, 0x01})
	confirm := connectionConfirm(0) // PROTOCOL_RDP: no TLS follows
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		got := make([]byte, len(request))
		if _, err := io.ReadFull(conn, got); err != nil {
			return
		}
		if _, err := conn.Write(confirm); err != nil {
			return
		}
		// Plain bytes from here, which is what the client will be sent.
		buf := make([]byte, 32)
		n, err := conn.Read(buf)
		if err != nil {
			return
		}
		_, _ = conn.Write(append([]byte("plain:"), buf[:n]...))
		time.Sleep(2 * time.Second)
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	client, page := socketPair()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Bridge(ctx, client, direct{}, Endpoint{Protocol: RDP, Port: port}, nil) }()

	req, err := asn1.Marshal(rdCleanPathPDU{
		Version:     rdCleanPathVersion,
		Destination: "host:3389",
		X224:        request,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Write(ctx, req); err != nil {
		t.Fatal(err)
	}

	var res rdCleanPathPDU
	if _, err := asn1.Unmarshal(page.readWithin(t, 5*time.Second), &res); err != nil {
		t.Fatalf("the response is not an RDCleanPath PDU: %v", err)
	}
	if res.Error.ErrorCode != 0 {
		t.Fatalf("the proxy reported error %d for a server that simply chose no TLS", res.Error.ErrorCode)
	}
	if len(res.ServerCertChain) != 0 {
		t.Errorf("there is no certificate to report without TLS, got %d", len(res.ServerCertChain))
	}
	if err := page.Write(ctx, []byte("rdp")); err != nil {
		t.Fatal(err)
	}
	if got := string(page.readWithin(t, 5*time.Second)); got != "plain:rdp" {
		t.Errorf("relay returned %q", got)
	}
}

func TestNegotiatedProtocol(t *testing.T) {
	cases := []struct {
		name    string
		confirm []byte
		want    uint32
	}{
		{"TLS selected", connectionConfirm(1), 1},
		{"CredSSP selected", connectionConfirm(3), 3},
		{"plain RDP selected", connectionConfirm(0), 0},
		{"no negotiation at all", tpkt([]byte{0x0e, 0xd0, 0, 0, 0x12, 0x34, 0x02}), 0},
		{"the server refused everything", failureConfirm(), 0},
	}
	for _, c := range cases {
		if got := negotiatedProtocol(c.confirm); got != c.want {
			t.Errorf("%s: negotiatedProtocol = %#x, want %#x", c.name, got, c.want)
		}
	}
}

// A client speaking a version this proxy does not gets told so in the shape it
// knows how to render, rather than a closed socket it has to guess about.
func TestBridgeRDPRejectsWrongVersion(t *testing.T) {
	client, page := socketPair()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Bridge(ctx, client, direct{}, Endpoint{Protocol: RDP, Port: 3389}, nil) }()

	req, err := asn1.Marshal(rdCleanPathPDU{Version: 1, X224: tpkt([]byte{0x0e, 0xe0})})
	if err != nil {
		t.Fatal(err)
	}
	if err := page.Write(ctx, req); err != nil {
		t.Fatal(err)
	}

	var res rdCleanPathPDU
	if _, err := asn1.Unmarshal(page.readWithin(t, 3*time.Second), &res); err != nil {
		t.Fatalf("the refusal is not an RDCleanPath PDU: %v", err)
	}
	if res.Error.ErrorCode != rdCleanPathGeneralError {
		t.Errorf("error code = %d", res.Error.ErrorCode)
	}
	if err := <-done; err == nil {
		t.Error("the bridge reported success for a session it refused")
	}
}

// A PDU split across messages is still one PDU: a WebSocket may break a
// message anywhere, and DER says how long a value is in its own header.
func TestReadCleanPathAcrossMessages(t *testing.T) {
	whole, err := asn1.Marshal(rdCleanPathPDU{
		Version:     rdCleanPathVersion,
		Destination: "host:3389",
		X224:        tpkt(make([]byte, 200)), // long enough for a multi-byte DER length
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(whole) < 0x80 {
		t.Fatalf("the fixture is too short to exercise long-form lengths (%d bytes)", len(whole))
	}

	client, page := socketPair()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < len(whole); i += 7 {
		end := min(i+7, len(whole))
		if err := page.Write(ctx, whole[i:end]); err != nil {
			t.Fatal(err)
		}
	}
	pdu, err := readCleanPath(ctx, client)
	if err != nil {
		t.Fatalf("reassembly failed: %v", err)
	}
	if pdu.Destination != "host:3389" || len(pdu.X224) != 204 {
		t.Errorf("reassembled %+v", pdu)
	}
}

func TestDERLength(t *testing.T) {
	cases := []struct {
		in    []byte
		total int
		ok    bool
	}{
		{[]byte{0x30}, 0, false},                    // no length byte yet
		{[]byte{0x30, 0x05}, 7, true},               // short form
		{[]byte{0x30, 0x82, 0x01}, 0, false},        // long form, incomplete
		{[]byte{0x30, 0x82, 0x01, 0x00}, 260, true}, // long form, two length bytes
		{[]byte{0x30, 0x80}, 0, false},              // indefinite length is not DER
	}
	for _, c := range cases {
		total, ok := derLength(c.in)
		if ok != c.ok || (ok && total != c.total) {
			t.Errorf("derLength(%x) = %d, %v; want %d, %v", c.in, total, ok, c.total, c.ok)
		}
	}
}

// direct dials this machine, standing in for the SSH channel that dials the
// host in production.
type direct struct{}

func (direct) Dial(network, address string) (net.Conn, error) {
	return net.DialTimeout(network, address, 3*time.Second)
}

// tpkt wraps a payload in the four-byte header every RDP connection frame
// carries: version 3, a reserved byte, and the total length.
func tpkt(payload []byte) []byte {
	out := make([]byte, 4+len(payload))
	out[0] = 3
	binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
	copy(out[4:], payload)
	return out
}

// connectionConfirm is what a server answers with: the X.224 confirm and, on
// the end, which security protocol it picked.
func connectionConfirm(selected uint32) []byte {
	body := []byte{0x0e, 0xd0, 0, 0, 0x12, 0x34, 0x00, 0x02, 0x01, 0x08, 0x00, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(body[11:], selected)
	return tpkt(body)
}

// failureConfirm is the other answer: the server refused every protocol.
func failureConfirm() []byte {
	return tpkt([]byte{0x0e, 0xd0, 0, 0, 0x12, 0x34, 0x00, 0x03, 0x00, 0x08, 0x00, 0x02, 0, 0, 0})
}

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-rdp-server"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// A viewer that stops answering ends the session, rather than holding the
// host's SSH connection open until TCP notices on its own.
func TestRelayEndsWhenTheViewerStopsAnswering(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			// Hold it open and say nothing: a live desktop nobody is touching.
			time.Sleep(10 * time.Second)
			conn.Close()
		}
	}()

	// Fast enough to test, for the same reason it is slow in production.
	defer func(every, timeout time.Duration) { pingEvery, pingTimeout = every, timeout }(pingEvery, pingTimeout)
	pingEvery, pingTimeout = 100*time.Millisecond, 300*time.Millisecond

	client, _ := socketPair()
	dead := &deafSocket{pipeSocket: client}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	done := make(chan error, 1)
	port := ln.Addr().(*net.TCPAddr).Port
	go func() { done <- Bridge(ctx, dead, direct{}, Endpoint{Protocol: VNC, Port: port}, nil) }()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "stopped answering") {
			t.Errorf("session ended with %v, want the heartbeat's reason", err)
		}
	case <-time.After(8 * time.Second):
		t.Error("a viewer that never answers a ping held the session open")
	}
}

// deafSocket answers no pings, which is what a socket to a machine that has
// gone away looks like before TCP admits it.
type deafSocket struct {
	*pipeSocket
}

func (d *deafSocket) Ping(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

// The endpoint is worth remembering once it answers, and not before: a port
// nobody is listening on must not become the one offered first next time.
func TestBridgeReportsOnlyEndpointsThatAnswer(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		if c, err := ln.Accept(); err == nil {
			time.Sleep(time.Second)
			c.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	reached := make(chan struct{}, 1)
	client, _ := socketPair()
	go func() {
		_ = Bridge(ctx, client, direct{}, Endpoint{Protocol: VNC, Port: port},
			func() { reached <- struct{}{} })
	}()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("a listening endpoint was never reported as reached")
	}

	// And the port nothing is behind.
	ln.Close()
	client2, _ := socketPair()
	never := make(chan struct{}, 1)
	err = Bridge(ctx, client2, direct{}, Endpoint{Protocol: VNC, Port: port},
		func() { never <- struct{}{} })
	if err == nil {
		t.Fatal("dialling a closed port should fail")
	}
	select {
	case <-never:
		t.Error("an endpoint that refused the connection was reported as reached")
	default:
	}
}
