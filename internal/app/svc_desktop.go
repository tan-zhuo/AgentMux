package app

import (
	"fmt"
	"net"
	"sync"
	"time"

	"agentmux/internal/desktop"
)

// SettingDesktopPrefix names the per-host setting that remembers which desktop
// a machine serves. The host's id completes the key.
const SettingDesktopPrefix = "desktop."

// probeTimeout bounds each dial the probe makes. A desktop that is listening
// answers in a round trip; anything slower than this is a port being filtered,
// and waiting longer would only make an absent desktop take longer to report.
const probeTimeout = 3 * time.Second

// DesktopService opens a host's screen.
//
// The screen itself is drawn by whatever viewer this computer already has —
// Remote Desktop Connection, Screen Sharing, Remmina. What AgentMux supplies is
// the part a viewer cannot: a private path to a desktop listening on the far
// side's loopback, through the SSH connection it is already holding. The host
// exposes nothing to the network and the desktop's own port stays where its
// administrator put it.
type DesktopService struct {
	core *Core

	mu       sync.Mutex
	forwards map[string]*desktop.Forward
	// Permission to open one in-app session, and the listener the desktop app
	// hands those sessions to. Both are nil until something asks.
	tickets  map[string]ticket
	loopback net.Listener
	// Set by the build that needs a listener, so a failure to open one is a
	// clear answer rather than a URL that cannot work.
	wantsLoopback bool
}

// NewDesktopService binds a desktop service to the core.
func NewDesktopService(c *Core) *DesktopService {
	return &DesktopService{core: c, forwards: map[string]*desktop.Forward{}}
}

// ServiceName identifies the service in Wails logs.
func (d *DesktopService) ServiceName() string { return "DesktopService" }

// Offer is what a host can show, and what it was last shown with.
type Offer struct {
	// Found is what answered a probe just now, in a stable order.
	Found []desktop.Endpoint `json:"found"`
	// Saved is the endpoint this host was opened with before, if any. It wins
	// over the probe: somebody chose it, and a machine serving both protocols
	// should not change its mind between openings.
	Saved *desktop.Endpoint `json:"saved"`
	// Reachable says the host answered at all. Without it, an empty Found means
	// "could not look" rather than "nothing there", and the two deserve
	// different sentences.
	Reachable bool `json:"reachable"`
}

// Probe asks a host which desktops it is serving.
func (d *DesktopService) Probe(serverID string) (Offer, error) {
	offer := Offer{Found: []desktop.Endpoint{}}
	if saved, ok := d.saved(serverID); ok {
		offer.Saved = &saved
	}
	if d.core.IsLocalAny(serverID) {
		// This is the computer whose screen is already in front of you.
		return offer, fmt.Errorf("this host is this computer")
	}
	lease, err := d.core.Pool.Acquire(serverID)
	if err != nil {
		return offer, err
	}
	defer lease.Release()

	offer.Reachable = true
	offer.Found = desktop.Probe(lease.Client, probeTimeout)
	return offer, nil
}

// Session is an open door and the viewer that was pointed at it.
type Session struct {
	Endpoint desktop.Endpoint `json:"endpoint"`
	// Local is the address on this computer that leads to the host's desktop.
	Local string `json:"local"`
	// Client is the viewer that opened, named so the answer is "Screen Sharing
	// opened" rather than "something happened".
	Client string `json:"client"`
}

// Open forwards a port to the host's desktop and starts this computer's viewer
// on it. The endpoint is remembered, so opening the same host again skips both
// the probe and the question.
func (d *DesktopService) Open(serverID string, ep desktop.Endpoint) (Session, error) {
	if !ep.Valid() {
		return Session{}, fmt.Errorf("%s is not a desktop endpoint", ep)
	}
	if d.core.IsLocalAny(serverID) {
		return Session{}, fmt.Errorf("this host is this computer")
	}
	remote := fmt.Sprintf("127.0.0.1:%d", ep.Port)

	fwd, err := d.forwardTo(serverID, remote)
	if err != nil {
		return Session{}, err
	}
	client, err := desktop.Launch(ep.Protocol, fwd.Local)
	if err != nil {
		// The door is left open: the viewer may be missing, but the forward is
		// good and the address is worth handing back so somebody can point
		// their own client at it.
		return Session{Endpoint: ep, Local: fwd.Local}, err
	}
	if err := d.core.Store.SetSetting(SettingDesktopPrefix+serverID, ep.String()); err != nil {
		return Session{}, err
	}
	return Session{Endpoint: ep, Local: fwd.Local, Client: client}, nil
}

// Close shuts a host's door before it times out on its own.
func (d *DesktopService) Close(serverID string) {
	d.mu.Lock()
	fwd := d.forwards[serverID]
	delete(d.forwards, serverID)
	d.mu.Unlock()
	if fwd != nil {
		_ = fwd.Close()
	}
}

// Forget drops the remembered endpoint, so the next opening asks again.
func (d *DesktopService) Forget(serverID string) error {
	return d.core.Store.SetSetting(SettingDesktopPrefix+serverID, "")
}

// forwardTo reuses the door this host already has, when it leads to the same
// place and has not timed itself out. Two viewers on one desktop is a thing
// people do; two forwards for it is not.
func (d *DesktopService) forwardTo(serverID, remote string) (*desktop.Forward, error) {
	d.mu.Lock()
	if fwd, ok := d.forwards[serverID]; ok {
		if !fwd.Closed() && fwd.Remote == remote {
			d.mu.Unlock()
			return fwd, nil
		}
		delete(d.forwards, serverID)
		go fwd.Close()
	}
	d.mu.Unlock()

	// The lease is held for as long as the door is open, which is what stops
	// the pool reaping the connection out from under a live desktop.
	lease, err := d.core.Pool.Acquire(serverID)
	if err != nil {
		return nil, err
	}
	fwd, err := desktop.Open(lease.Client, remote, lease.Release)
	if err != nil {
		lease.Release()
		return nil, err
	}
	d.mu.Lock()
	d.forwards[serverID] = fwd
	d.mu.Unlock()
	return fwd, nil
}

func (d *DesktopService) saved(serverID string) (desktop.Endpoint, bool) {
	return desktop.ParseEndpoint(d.core.Store.GetSetting(SettingDesktopPrefix+serverID, ""))
}
