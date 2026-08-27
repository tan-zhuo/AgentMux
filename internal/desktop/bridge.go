package desktop

// Carrying a desktop session to a viewer that runs inside AgentMux itself.
//
// The forward in this package hands a TCP port to a viewer the computer
// already has. That cannot work in a browser, on a phone, or in a pane of this
// app: none of them can open a TCP socket. What they can open is a WebSocket,
// so this is the other end of the same idea — the desktop arrives as WebSocket
// messages, and the viewer is a client compiled to run in the page.
//
// VNC needs nothing but a relay: the client speaks RFB, and RFB over a
// WebSocket is RFB with frame boundaries nobody has to respect.
//
// RDP needs a conversation first. A browser cannot do the TLS handshake with
// the RDP server itself — it has no socket to do it on — so the client sends
// its X.224 connection request here, and this end performs the handshake and
// answers with the server's certificate chain. That exchange is RDCleanPath,
// the protocol IronRDP's web client speaks, and after it the two ends are back
// to relaying bytes.

import (
	"context"
	"crypto/tls"
	"encoding/asn1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// Socket is the message half of a WebSocket, as this package needs it.
//
// An interface rather than the websocket type itself, so the bridge can be
// driven by a pipe in a test: everything below is protocol, and protocol is
// the part worth testing without a network.
type Socket interface {
	// Read returns the next binary message.
	Read(ctx context.Context) ([]byte, error)
	// Write sends one binary message.
	Write(ctx context.Context, b []byte) error
}

// handshakeTimeout bounds the part of a session that should be immediate. The
// relay that follows has no timeout at all: a desktop nobody is touching is
// idle, not stuck.
const handshakeTimeout = 20 * time.Second

// How often the viewer is asked whether it is still there, and how long it has
// to answer.
//
// A desktop can be watched for an hour without a byte travelling in either
// direction, so silence proves nothing — and the socket is hijacked out of the
// HTTP server, which means nothing else is watching it either. A phone that
// drives into a tunnel would otherwise hold this host's SSH connection open
// until TCP gave up on its own, which can be never.
var (
	pingEvery   = 30 * time.Second
	pingTimeout = 10 * time.Second
)

// Pinger is a Socket that can ask the far end whether it is still listening.
// Sockets that cannot are simply not asked.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Bridge carries one desktop session between a socket and a host, dialling the
// host through d — an SSH channel in production.
//
// reached, when given, is called once the endpoint has answered. That is the
// moment worth remembering a choice by: a port somebody typed wrongly refuses
// the connection, and an endpoint that refused is not the one to offer first
// next time.
func Bridge(ctx context.Context, ws Socket, d Dialer, ep Endpoint, reached func()) error {
	if !ep.Valid() {
		return fmt.Errorf("%s is not a desktop endpoint", ep)
	}
	addr := fmt.Sprintf("127.0.0.1:%d", ep.Port)
	if ep.Protocol == RDP {
		return bridgeRDP(ctx, ws, d, addr, reached)
	}
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	if reached != nil {
		reached()
	}
	return relay(ctx, ws, conn)
}

// relay pumps bytes both ways until either end is done. Message boundaries
// carry no meaning in either protocol — both are streams — so reads are sent
// on as they arrive, at whatever size they arrive in.
func relay(ctx context.Context, ws Socket, conn net.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Three: both pumps and the heartbeat, so whichever ends first can say so
	// without waiting for the others to be read.
	errs := make(chan error, 3)
	if p, ok := ws.(Pinger); ok {
		go func() {
			tick := time.NewTicker(pingEvery)
			defer tick.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-tick.C:
					// The pong is read by the pump below, which is always
					// reading, so this only ever waits on the far end.
					ask, cancelPing := context.WithTimeout(ctx, pingTimeout)
					err := p.Ping(ask)
					cancelPing()
					if err != nil {
						errs <- fmt.Errorf("the viewer stopped answering: %w", err)
						return
					}
				}
			}
		}()
	}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if werr := ws.Write(ctx, buf[:n]); werr != nil {
					errs <- werr
					return
				}
			}
			if err != nil {
				errs <- err
				return
			}
		}
	}()
	go func() {
		for {
			msg, err := ws.Read(ctx)
			if err != nil {
				errs <- err
				return
			}
			if _, err := conn.Write(msg); err != nil {
				errs <- err
				return
			}
		}
	}()

	err := <-errs
	_ = conn.Close()
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// --- RDCleanPath ------------------------------------------------------------

// rdCleanPathVersion is the only version of the PDU there is.
const rdCleanPathVersion = 3390

// General and negotiation are the two error codes the client knows how to
// describe to whoever is looking at it.
const (
	rdCleanPathGeneralError     = 1
	rdCleanPathNegotiationError = 2
)

// rdCleanPathErr is the proxy's side of a failure, in the shape the client
// renders. The optional numbers are omitted when they are zero, which is what
// "the proxy has nothing to add" looks like on the wire.
type rdCleanPathErr struct {
	ErrorCode      int `asn1:"explicit,tag:0"`
	HTTPStatusCode int `asn1:"optional,explicit,tag:1"`
	WSALastError   int `asn1:"optional,explicit,tag:2"`
	TLSAlertCode   int `asn1:"optional,explicit,tag:3"`
}

// rdCleanPathPDU is both halves of the exchange: the client fills in the
// destination and its X.224 request, the proxy answers with the X.224 confirm,
// the certificate chain and the address it actually reached. Every optional
// field is left at its zero value when it does not apply, which is how Go's
// DER encoder knows to leave it out.
//
// Tag 8 is skipped: it was an OCSP response the protocol never shipped.
type rdCleanPathPDU struct {
	Version           int            `asn1:"explicit,tag:0"`
	Error             rdCleanPathErr `asn1:"optional,explicit,tag:1"`
	Destination       string         `asn1:"optional,explicit,tag:2,utf8"`
	ProxyAuth         string         `asn1:"optional,explicit,tag:3,utf8"`
	ServerAuth        string         `asn1:"optional,explicit,tag:4,utf8"`
	PreconnectionBlob string         `asn1:"optional,explicit,tag:5,utf8"`
	X224              []byte         `asn1:"optional,explicit,tag:6"`
	ServerCertChain   [][]byte       `asn1:"optional,explicit,tag:7"`
	ServerAddr        string         `asn1:"optional,explicit,tag:9,utf8"`
}

// bridgeRDP performs the RDCleanPath exchange and then relays the session.
//
// The client cannot be given a socket, so this end does what a socket would
// have been used for: it carries the X.224 negotiation, performs the TLS
// handshake, and reports what the server presented. Everything after that is
// the RDP session, which travels inside the TLS connection opened here and
// inside the WebSocket's own encryption on the other side.
func bridgeRDP(ctx context.Context, ws Socket, d Dialer, addr string, reached func()) error {
	hs, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()

	req, err := readCleanPath(hs, ws)
	if err != nil {
		return err
	}
	if req.Version != rdCleanPathVersion {
		return writeCleanPathError(hs, ws, rdCleanPathGeneralError,
			fmt.Errorf("client speaks RDCleanPath version %d, this proxy speaks %d",
				req.Version, rdCleanPathVersion))
	}
	if len(req.X224) == 0 {
		// VMConnect's variant carries a preconnection blob and no X.224. It
		// belongs to Hyper-V's console port, which is not something reached
		// through a host's own loopback.
		return writeCleanPathError(hs, ws, rdCleanPathGeneralError,
			errors.New("this proxy only handles the X.224 form of RDCleanPath"))
	}

	// The destination the client names is advisory: where this goes is the
	// endpoint the user chose, on the host whose SSH connection is dialling.
	// Honouring the client's string would turn a desktop session into an open
	// relay to anything the host can reach.
	conn, err := d.Dial("tcp", addr)
	if err != nil {
		return writeCleanPathError(hs, ws, rdCleanPathGeneralError, err)
	}
	defer conn.Close()

	confirm, err := x224Exchange(hs, conn, req.X224)
	if err != nil {
		return writeCleanPathError(hs, ws, rdCleanPathNegotiationError, err)
	}
	// It spoke RDP back, which is as much proof as this endpoint can give.
	if reached != nil {
		reached()
	}

	// What happens next is the server's decision, not ours. The confirm says
	// which security protocol it picked, and every one of them except plain
	// RDP security begins with a TLS handshake. Starting one anyway is how a
	// server that answered "no TLS" — an xrdp without a certificate, an old
	// box, a negotiation that failed — turned into "first record does not look
	// like a TLS handshake", which describes this end's mistake rather than
	// anything the server did.
	session := conn
	chain := [][]byte{}
	if negotiatedProtocol(confirm) != protocolRDP {
		// The certificate is not checked here, and that is the design rather
		// than an omission: this hop is inside the SSH connection already, and
		// the chain goes to the client, which is the end that knows what it
		// trusts — and which needs the server's public key for network level
		// authentication in any case.
		tlsConn := tls.Client(conn, &tls.Config{
			InsecureSkipVerify: true, //nolint:gosec // the chain is forwarded to the client to judge
			// Old Windows servers still negotiate TLS 1.0, and refusing them
			// here would refuse the machines most likely to be reached this way.
			MinVersion: tls.VersionTLS10,
		})
		if err := tlsConn.HandshakeContext(hs); err != nil {
			return writeCleanPathError(hs, ws, rdCleanPathGeneralError, err)
		}
		for _, cert := range tlsConn.ConnectionState().PeerCertificates {
			chain = append(chain, cert.Raw)
		}
		session = tlsConn
	}

	if err := writeCleanPath(hs, ws, rdCleanPathPDU{
		Version:         rdCleanPathVersion,
		X224:            confirm,
		ServerCertChain: chain,
		ServerAddr:      remoteAddr(conn, addr),
	}); err != nil {
		return err
	}

	return relay(ctx, ws, session)
}

// The security protocols a server can select in its connection confirm. Only
// the first means "no TLS"; the rest all begin with a TLS handshake, and
// differ in what happens inside it.
const (
	protocolRDP      uint32 = 0x00
	negotiationRsp   byte   = 0x02
	negotiationBytes        = 19 // a confirm with a negotiation structure on the end
)

// negotiatedProtocol reads the server's choice out of the connection confirm.
//
// A confirm with nothing on the end is a server old enough to predate the
// negotiation, and a failure response is the server refusing every protocol
// offered — both mean this end must not start a TLS handshake, and both are
// the client's to explain, since the confirm reaches it verbatim.
func negotiatedProtocol(confirm []byte) uint32 {
	if len(confirm) < negotiationBytes || confirm[11] != negotiationRsp {
		return protocolRDP
	}
	return binary.LittleEndian.Uint32(confirm[15:19])
}

// x224Exchange sends the client's connection request and reads the server's
// confirm, which is one TPKT frame: version, reserved, then a big-endian
// length that counts the header itself.
func x224Exchange(ctx context.Context, conn net.Conn, request []byte) ([]byte, error) {
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
		defer func() { _ = conn.SetDeadline(time.Time{}) }()
	}
	if _, err := conn.Write(request); err != nil {
		return nil, fmt.Errorf("send the connection request: %w", err)
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return nil, fmt.Errorf("read the connection confirm: %w", err)
	}
	if header[0] != 3 {
		return nil, fmt.Errorf("the server answered with TPKT version %d, which is not RDP", header[0])
	}
	total := int(binary.BigEndian.Uint16(header[2:4]))
	if total < len(header) || total > 4096 {
		return nil, fmt.Errorf("the server announced a %d byte connection confirm", total)
	}
	confirm := make([]byte, total)
	copy(confirm, header)
	if _, err := io.ReadFull(conn, confirm[len(header):]); err != nil {
		return nil, fmt.Errorf("read the connection confirm: %w", err)
	}
	return confirm, nil
}

// maxCleanPath is as large as a request has any business being. One carries a
// destination, a token and an X.224 connection request — a few hundred bytes.
// Without a ceiling, a client that never sends a parsable header is a client
// that grows this buffer until the machine gives out.
const maxCleanPath = 64 << 10

// readCleanPath reads messages until they add up to one complete PDU. A
// WebSocket may split anything anywhere, and DER says how long a value is in
// its own header, so the header is what decides when there is enough.
func readCleanPath(ctx context.Context, ws Socket) (rdCleanPathPDU, error) {
	var buf []byte
	for {
		msg, err := ws.Read(ctx)
		if err != nil {
			return rdCleanPathPDU{}, fmt.Errorf("read the RDCleanPath request: %w", err)
		}
		buf = append(buf, msg...)
		if len(buf) > maxCleanPath {
			return rdCleanPathPDU{}, fmt.Errorf("the RDCleanPath request passed %d bytes without becoming one", maxCleanPath)
		}
		total, ok := derLength(buf)
		if !ok {
			continue
		}
		if len(buf) < total {
			continue
		}
		var pdu rdCleanPathPDU
		if _, err := asn1.Unmarshal(buf[:total], &pdu); err != nil {
			return rdCleanPathPDU{}, fmt.Errorf("the RDCleanPath request is malformed: %w", err)
		}
		return pdu, nil
	}
}

// derLength reads how long the whole DER value is, header included, or reports
// that the header has not arrived yet.
func derLength(buf []byte) (int, bool) {
	if len(buf) < 2 {
		return 0, false
	}
	length := int(buf[1])
	if length < 0x80 {
		return 2 + length, true
	}
	count := length & 0x7f
	if count == 0 || count > 4 {
		// An indefinite or absurd length: DER has neither, and reading on
		// would only pile up bytes that will never parse.
		return 0, false
	}
	if len(buf) < 2+count {
		return 0, false
	}
	length = 0
	for _, b := range buf[2 : 2+count] {
		length = length<<8 | int(b)
	}
	return 2 + count + length, true
}

func writeCleanPath(ctx context.Context, ws Socket, pdu rdCleanPathPDU) error {
	raw, err := asn1.Marshal(pdu)
	if err != nil {
		return fmt.Errorf("encode the RDCleanPath response: %w", err)
	}
	return ws.Write(ctx, raw)
}

// writeCleanPathError tells the client why there is no session, and returns the
// same reason for the log. The client shows its own sentence for the code; the
// error here is the one worth keeping on this side.
func writeCleanPathError(ctx context.Context, ws Socket, code int, cause error) error {
	_ = writeCleanPath(ctx, ws, rdCleanPathPDU{
		Version: rdCleanPathVersion,
		Error:   rdCleanPathErr{ErrorCode: code},
	})
	return cause
}

// remoteAddr is the address the connection actually reached, which for a
// forward through SSH is not something the local socket knows. The endpoint is
// the honest answer there.
func remoteAddr(conn net.Conn, fallback string) string {
	if a := conn.RemoteAddr(); a != nil && a.String() != "" {
		return a.String()
	}
	return fallback
}
