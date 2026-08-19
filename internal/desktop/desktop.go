// Package desktop opens a remote machine's screen.
//
// It implements no screen protocol of its own. What it does is the part that is
// actually hard here: getting a private path to a desktop that is listening on
// the far side's loopback, using the SSH connection AgentMux already holds, and
// then handing that path to the client this computer already has. Nothing has
// to be exposed to the network, and no credentials are duplicated — reaching
// the host is still the SSH connection's problem, and it has already solved it.
package desktop

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Protocol is how a desktop is spoken to.
type Protocol string

const (
	// RDP is what Windows serves, and what GNOME serves in recent versions.
	RDP Protocol = "rdp"
	// VNC is what macOS Screen Sharing serves, and what the X and Wayland
	// servers on Linux are usually fronted by.
	VNC Protocol = "vnc"
)

// Endpoint is a desktop service on a host: a protocol, on a port.
type Endpoint struct {
	Protocol Protocol `json:"protocol"`
	Port     int      `json:"port"`
}

func (e Endpoint) String() string { return fmt.Sprintf("%s:%d", e.Protocol, e.Port) }

// Valid reports whether an endpoint is one this package can act on.
func (e Endpoint) Valid() bool {
	return (e.Protocol == RDP || e.Protocol == VNC) && e.Port > 0 && e.Port < 65536
}

// ParseEndpoint reads the stored form, "rdp:3389".
func ParseEndpoint(s string) (Endpoint, bool) {
	var e Endpoint
	var proto string
	if _, err := fmt.Sscanf(s, "%3s:%d", &proto, &e.Port); err != nil {
		return Endpoint{}, false
	}
	e.Protocol = Protocol(proto)
	return e, e.Valid()
}

// Usual is where a desktop tends to listen. Probing is done against this list
// rather than a range: a port scan of somebody's machine is not a reasonable
// thing for a tool to do on its own, and the three that matter cover Windows,
// macOS and every Linux desktop that ships a VNC or RDP server.
var Usual = []Endpoint{
	{Protocol: RDP, Port: 3389},
	{Protocol: VNC, Port: 5900},
	{Protocol: VNC, Port: 5901},
}

// Dialer opens a TCP connection as seen from the host — an SSH direct-tcpip
// channel in production, a plain dial in a test. It is the whole seam between
// this package and the connection machinery.
type Dialer interface {
	Dial(network, address string) (net.Conn, error)
}

// Probe reports which of the usual desktop ports answer on the host.
//
// Answering is the whole test: a TCP connection that opens to 3389 is a
// Terminal Service, and asking anything more of it would mean speaking the
// protocol. The dials go out together, because doing them one at a time on a
// host with nothing listening costs three timeouts in a row.
func Probe(d Dialer, timeout time.Duration) []Endpoint {
	type result struct {
		at int
		ok bool
	}
	out := make(chan result, len(Usual))
	for i, e := range Usual {
		go func(i int, e Endpoint) {
			conn, err := dialWithTimeout(d, fmt.Sprintf("127.0.0.1:%d", e.Port), timeout)
			if err == nil {
				_ = conn.Close()
			}
			out <- result{at: i, ok: err == nil}
		}(i, e)
	}
	found := make([]bool, len(Usual))
	for range Usual {
		r := <-out
		found[r.at] = r.ok
	}
	// Returned in the order they are listed rather than the order they
	// answered, so a host with both offers the same choice every time.
	var live []Endpoint
	for i, ok := range found {
		if ok {
			live = append(live, Usual[i])
		}
	}
	return live
}

// dialWithTimeout bounds a dial the Dialer itself may not bound. An SSH channel
// open to a port with nothing behind it can sit for as long as the far side's
// TCP stack takes to refuse it.
func dialWithTimeout(d Dialer, addr string, timeout time.Duration) (net.Conn, error) {
	type res struct {
		conn net.Conn
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		c, err := d.Dial("tcp", addr)
		ch <- res{c, err}
	}()
	select {
	case r := <-ch:
		return r.conn, r.err
	case <-time.After(timeout):
		// The dial is left to finish and close itself; abandoning the goroutine
		// is cheaper than holding the caller for a host that is not answering.
		go func() {
			if r := <-ch; r.conn != nil {
				_ = r.conn.Close()
			}
		}()
		return nil, fmt.Errorf("nothing answered %s in %s", addr, timeout)
	}
}

// Forward is a door on this computer that opens onto a port on the host.
//
// It listens on the loopback interface only. That still means any process
// running as any user on this machine can reach the desktop for as long as the
// door is open, which is exactly what `ssh -L` means and is why it closes
// itself once nobody is using it.
type Forward struct {
	// Local is the address a desktop client should be pointed at.
	Local string
	// Remote is the address on the host it leads to.
	Remote string

	ln      net.Listener
	dialer  Dialer
	onClose func()

	active   atomic.Int64
	lastUsed atomic.Int64
	closed   atomic.Bool
	done     chan struct{}
	once     sync.Once
}

// idleTTL closes a forward that nothing has used for this long, which releases
// the SSH lease holding the connection open. A desktop session that is still
// live keeps a connection through the door, so this only fires once the client
// has actually gone.
const idleTTL = 5 * time.Minute

// Open builds a forward to remote through d. onClose is called once, after the
// door is shut, and is where the caller releases whatever the dialer came from.
func Open(d Dialer, remote string, onClose func()) (*Forward, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	f := &Forward{
		Local:   ln.Addr().String(),
		Remote:  remote,
		ln:      ln,
		dialer:  d,
		onClose: onClose,
		done:    make(chan struct{}),
	}
	f.touch()
	go f.accept()
	go f.reapWhenIdle()
	return f, nil
}

func (f *Forward) touch() { f.lastUsed.Store(time.Now().UnixNano()) }

func (f *Forward) accept() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.active.Add(1)
		f.touch()
		go f.pipe(conn)
	}
}

func (f *Forward) pipe(local net.Conn) {
	defer func() {
		f.active.Add(-1)
		f.touch()
		_ = local.Close()
	}()
	remote, err := f.dialer.Dial("tcp", f.Remote)
	if err != nil {
		return
	}
	defer remote.Close()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = io.Copy(remote, local); closeWrite(remote) }()
	go func() { defer wg.Done(); _, _ = io.Copy(local, remote); closeWrite(local) }()
	wg.Wait()
}

// closeWrite half-closes where the connection allows it, so a client that has
// said everything it is going to say does not look like one that hung up.
func closeWrite(c net.Conn) {
	type halfCloser interface{ CloseWrite() error }
	if h, ok := c.(halfCloser); ok {
		_ = h.CloseWrite()
	}
}

func (f *Forward) reapWhenIdle() {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-f.done:
			return
		case <-tick.C:
			if f.active.Load() > 0 {
				continue
			}
			if time.Since(time.Unix(0, f.lastUsed.Load())) > idleTTL {
				_ = f.Close()
				return
			}
		}
	}
}

// Close shuts the door.
func (f *Forward) Close() error {
	err := errors.New("already closed")
	f.once.Do(func() {
		f.closed.Store(true)
		close(f.done)
		err = f.ln.Close()
		if f.onClose != nil {
			f.onClose()
		}
	})
	return err
}

// Closed reports whether this forward has shut itself, so a caller holding one
// knows to open another rather than handing out an address nothing listens on.
func (f *Forward) Closed() bool { return f.closed.Load() }
