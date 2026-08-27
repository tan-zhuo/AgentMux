package app

// The in-app half of the desktop feature: a WebSocket that carries a session
// to a viewer running inside AgentMux, rather than a TCP port handed to a
// viewer the computer already has.
//
// Both modes end up here. A served browser — a tablet, the phone — opens this
// on the same origin it loaded the app from. The desktop app cannot: its page
// is served by the webview from its own scheme, so it opens a listener on this
// machine's loopback and connects to that instead. Either way the session
// itself travels the same road as everything else: through the host's SSH
// connection, out of a port that is listening on the host's own loopback and
// exposed to nobody.

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"agentmux/internal/desktop"
)

// WSPath is where the bridge is mounted in serve mode.
const WSPath = "/api/desktop"

// ticketTTL is how long a ticket is worth presenting. It is issued and used in
// the same breath — the frontend asks for one and opens the socket — so this
// only has to survive a slow render.
const ticketTTL = 60 * time.Second

// ticket is permission to open exactly one session, on exactly one host, at
// exactly one endpoint.
//
// The parameters live here rather than in the URL so that holding the URL is
// not the same as being able to reach any port on any host: what the client
// presents is an opaque string, and what it gets is the session somebody
// already chose in the UI.
type ticket struct {
	serverID string
	endpoint desktop.Endpoint
	expires  time.Time
}

// InAppSession is what the frontend needs to open a viewer.
type InAppSession struct {
	// URL is where to point a WebSocket. Absolute for the desktop app, whose
	// page has no origin to be relative to; relative in serve mode, where the
	// browser knows the origin better than this end does — a reverse proxy, a
	// tunnel, a phone reaching a LAN address are all the same to it.
	URL string `json:"url"`
	// Protocol says which viewer to build.
	Protocol desktop.Protocol `json:"protocol"`
	// Destination is what an RDP client names as its target. It is not where
	// the connection goes — that is decided here, from the ticket — but the
	// protocol carries a destination and this is the honest one to carry.
	Destination string `json:"destination"`
}

// InApp issues a ticket and says where to present it.
func (d *DesktopService) InApp(serverID string, ep desktop.Endpoint) (InAppSession, error) {
	if !ep.Valid() {
		return InAppSession{}, fmt.Errorf("%s is not a desktop endpoint", ep)
	}
	if d.core.IsLocalAny(serverID) {
		return InAppSession{}, fmt.Errorf("this host is this computer")
	}

	id, err := randomID()
	if err != nil {
		return InAppSession{}, err
	}
	d.mu.Lock()
	if d.tickets == nil {
		d.tickets = map[string]ticket{}
	}
	// Sweep on issue: tickets are rare and short-lived, so there is nothing
	// here worth a goroutine of its own.
	for k, t := range d.tickets {
		if time.Now().After(t.expires) {
			delete(d.tickets, k)
		}
	}
	d.tickets[id] = ticket{serverID: serverID, endpoint: ep, expires: time.Now().Add(ticketTTL)}
	d.mu.Unlock()

	base := WSPath
	if d.loopback != nil {
		base = "ws://" + d.loopback.Addr().String() + WSPath
	}
	return InAppSession{
		URL:         base + "?ticket=" + id,
		Protocol:    ep.Protocol,
		Destination: fmt.Sprintf("127.0.0.1:%d", ep.Port),
	}, nil
}

// claim spends a ticket. A ticket opens one session: presenting it twice is
// either a bug or somebody else holding a copy, and both deserve the same no.
func (d *DesktopService) claim(id string) (ticket, bool) {
	if id == "" {
		return ticket{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, t := range d.tickets {
		if subtle.ConstantTimeCompare([]byte(k), []byte(id)) == 1 {
			delete(d.tickets, k)
			return t, time.Now().Before(t.expires)
		}
	}
	return ticket{}, false
}

// ServeWS is the endpoint itself: it takes a ticket, holds the host's SSH
// connection for as long as the session lasts, and carries the desktop.
func (d *DesktopService) ServeWS(w http.ResponseWriter, r *http.Request) {
	tk, ok := d.claim(r.URL.Query().Get("ticket"))
	if !ok {
		http.Error(w, "this desktop ticket is not valid", http.StatusForbidden)
		return
	}

	lease, err := d.core.Pool.Acquire(tk.serverID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer lease.Release()

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		// The page that opens this is the app's own, and in the desktop build
		// it is served from a scheme with no origin to compare against. The
		// ticket is what says this session was asked for.
		InsecureSkipVerify: true,
		CompressionMode:    websocket.CompressionDisabled,
	})
	if err != nil {
		return
	}
	// A desktop sends frames, and a frame can be large. The default limit is
	// sized for chat.
	conn.SetReadLimit(8 << 20)

	ctx := r.Context()
	// Remembered only once the endpoint answers, the same way opening one in a
	// system viewer is remembered: whichever way a desktop was reached, that
	// is the one to offer first next time — and a port that refused is not.
	remember := func() {
		_ = d.core.Store.SetSetting(SettingDesktopPrefix+tk.serverID, tk.endpoint.String())
	}
	err = desktop.Bridge(ctx, wsSocket{conn}, lease.Client, tk.endpoint, remember)
	if err != nil {
		// Logged as well as reported: the socket's close reason is capped at a
		// sentence and is the last thing a viewer sees before it disappears,
		// which is no help at all to whoever has to work out why.
		log.Printf("desktop %s on %s ended: %v", tk.endpoint, tk.serverID, err)
		conn.Close(websocket.StatusInternalError, truncate(err.Error(), 120))
		return
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

// EnableLoopback opens the listener the desktop app connects to, and is called
// only by that build. In serve mode there is already an HTTP server, and the
// browser is not on this machine in the first place.
func (d *DesktopService) EnableLoopback() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loopback != nil {
		return nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	d.loopback = ln
	mux := http.NewServeMux()
	mux.HandleFunc(WSPath, d.ServeWS)
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	return nil
}

// wsSocket adapts a WebSocket to the message pair the bridge works in.
type wsSocket struct{ c *websocket.Conn }

func (s wsSocket) Read(ctx context.Context) ([]byte, error) {
	_, b, err := s.c.Read(ctx)
	return b, err
}

func (s wsSocket) Write(ctx context.Context, b []byte) error {
	return s.c.Write(ctx, websocket.MessageBinary, b)
}

// Ping is how the bridge notices a viewer that went away without saying so.
func (s wsSocket) Ping(ctx context.Context) error { return s.c.Ping(ctx) }

func randomID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// truncate keeps a close reason inside the 123 bytes the protocol allows.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
