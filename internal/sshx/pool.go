package sshx

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Target is everything the pool needs to open one connection. Secrets are
// passed in memory only; they are never logged.
type Target struct {
	ServerID     string
	Name         string
	Host         string
	Port         int
	User         string
	AuthType     string
	KeyPath      string
	Password     string
	Passphrase   string
	JumpServerID string
	HostKey      string // pinned "<type> <base64>" or "" for trust-on-first-use
}

// Resolver hands the pool credentials and receives trust decisions back.
type Resolver interface {
	Resolve(serverID string) (Target, error)
	PinHostKey(serverID, hostKey string) error
	MarkOK(serverID string)
}

// ConnState is broadcast whenever a server connection changes state.
type ConnState struct {
	ServerID string `json:"serverId"`
	State    string `json:"state"` // connecting | connected | disconnected | error
	Detail   string `json:"detail"`
	At       int64  `json:"at"`
}

// ExecResult is the outcome of a one-shot remote command. A non-zero Code is
// data, not an error — tmux leans on exit codes for existence checks.
type ExecResult struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
}

const (
	dialTimeout    = 15 * time.Second
	keepaliveEvery = 30 * time.Second
	reapEvery      = 30 * time.Second
)

// Lease is a borrowed reference to a pooled client. Release exactly once.
type Lease struct {
	Client   *ssh.Client
	release  func()
	relOnce  sync.Once
	ServerID string
}

// Release returns the lease to the pool.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.relOnce.Do(func() {
		if l.release != nil {
			l.release()
		}
	})
}

type slot struct {
	serverID  string
	mu        sync.Mutex
	client    *ssh.Client
	refs      int
	lastUsed  time.Time
	jumpLease *Lease
	stopKeep  chan struct{}
}

// Pool multiplexes many logical sessions over one SSH connection per server —
// the native equivalent of OpenSSH's ControlMaster — and reaps idle links so
// that a hundred configured servers do not mean a hundred live sockets.
type Pool struct {
	res     Resolver
	idleTTL time.Duration
	onState func(ConnState)

	mu    sync.Mutex
	slots map[string]*slot

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewPool builds a pool. idleTTL of 0 disables idle reaping.
func NewPool(res Resolver, idleTTL time.Duration, onState func(ConnState)) *Pool {
	p := &Pool{
		res:     res,
		idleTTL: idleTTL,
		onState: onState,
		slots:   map[string]*slot{},
		stopCh:  make(chan struct{}),
	}
	go p.reapLoop()
	return p
}

// Stop closes every pooled connection.
func (p *Pool) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopCh)
		p.mu.Lock()
		slots := make([]*slot, 0, len(p.slots))
		for _, sl := range p.slots {
			slots = append(slots, sl)
		}
		p.mu.Unlock()
		for _, sl := range slots {
			sl.mu.Lock()
			sl.teardownLocked()
			sl.mu.Unlock()
		}
	})
}

func (p *Pool) emit(serverID, state, detail string) {
	if p.onState != nil {
		p.onState(ConnState{ServerID: serverID, State: state, Detail: detail, At: time.Now().Unix()})
	}
}

func (p *Pool) slotFor(serverID string) *slot {
	p.mu.Lock()
	defer p.mu.Unlock()
	sl, ok := p.slots[serverID]
	if !ok {
		sl = &slot{serverID: serverID}
		p.slots[serverID] = sl
	}
	return sl
}

// Acquire borrows a live connection to serverID, dialling if necessary.
func (p *Pool) Acquire(serverID string) (*Lease, error) {
	return p.acquire(serverID, nil)
}

func (p *Pool) acquire(serverID string, chain []string) (*Lease, error) {
	for _, id := range chain {
		if id == serverID {
			return nil, fmt.Errorf("jump host chain loops back to server %s", serverID)
		}
	}
	if len(chain) > 8 {
		return nil, errors.New("jump host chain is deeper than 8 hops")
	}

	t, err := p.res.Resolve(serverID)
	if err != nil {
		return nil, err
	}

	sl := p.slotFor(serverID)
	sl.mu.Lock()
	defer sl.mu.Unlock()

	if sl.client != nil {
		if isAlive(sl.client) {
			sl.refs++
			sl.lastUsed = time.Now()
			return p.leaseLocked(sl), nil
		}
		p.emit(serverID, "disconnected", "connection went away, redialling")
		sl.teardownLocked()
	}

	p.emit(serverID, "connecting", t.Host)
	client, jumpLease, err := p.dial(t, append(chain, serverID))
	if err != nil {
		p.emit(serverID, "error", err.Error())
		return nil, err
	}

	sl.client = client
	sl.jumpLease = jumpLease
	sl.refs = 1
	sl.lastUsed = time.Now()
	sl.stopKeep = make(chan struct{})
	go p.keepalive(sl, client, sl.stopKeep)

	p.res.MarkOK(serverID)
	p.emit(serverID, "connected", t.Host)
	return p.leaseLocked(sl), nil
}

func (p *Pool) leaseLocked(sl *slot) *Lease {
	client := sl.client
	return &Lease{
		Client:   client,
		ServerID: sl.serverID,
		release: func() {
			sl.mu.Lock()
			defer sl.mu.Unlock()
			if sl.refs > 0 {
				sl.refs--
			}
			sl.lastUsed = time.Now()
		},
	}
}

func (p *Pool) dial(t Target, chain []string) (*ssh.Client, *Lease, error) {
	cfg, err := p.clientConfig(t)
	if err != nil {
		return nil, nil, err
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(t.Port))

	if t.JumpServerID == "" {
		conn, err := net.DialTimeout("tcp", addr, dialTimeout)
		if err != nil {
			return nil, nil, fmt.Errorf("dial %s: %w", addr, err)
		}
		_ = conn.SetDeadline(time.Now().Add(dialTimeout))
		c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
		if err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		_ = conn.SetDeadline(time.Time{})
		return ssh.NewClient(c, chans, reqs), nil, nil
	}

	jl, err := p.acquire(t.JumpServerID, chain)
	if err != nil {
		return nil, nil, fmt.Errorf("jump host: %w", err)
	}
	conn, err := jl.Client.Dial("tcp", addr)
	if err != nil {
		jl.Release()
		return nil, nil, fmt.Errorf("dial %s via jump host: %w", addr, err)
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		jl.Release()
		return nil, nil, err
	}
	return ssh.NewClient(c, chans, reqs), jl, nil
}

func (p *Pool) clientConfig(t Target) (*ssh.ClientConfig, error) {
	methods, err := authMethods(t)
	if err != nil {
		return nil, err
	}
	return &ssh.ClientConfig{
		User:            t.User,
		Auth:            methods,
		Timeout:         dialTimeout,
		HostKeyCallback: p.hostKeyCallback(t),
	}, nil
}

// hostKeyCallback implements trust-on-first-use: the first key seen is pinned,
// and any later mismatch is a hard failure the user must resolve explicitly.
func (p *Pool) hostKeyCallback(t Target) ssh.HostKeyCallback {
	return func(hostname string, _ net.Addr, key ssh.PublicKey) error {
		fingerprint := key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal())
		if t.HostKey == "" {
			return p.res.PinHostKey(t.ServerID, fingerprint)
		}
		if t.HostKey != fingerprint {
			return fmt.Errorf(
				"host key for %s does not match the pinned key — this may be a man-in-the-middle attack. "+
					"If you rotated the server's key, clear the pinned key in server settings first "+
					"(pinned %s, offered %s)",
				hostname, ssh.FingerprintSHA256(mustParse(t.HostKey)), ssh.FingerprintSHA256(key))
		}
		return nil
	}
}

// mustParse converts a stored pin back to a PublicKey for display. A corrupt
// pin degrades to nil, which FingerprintSHA256 tolerates via the fallback below.
func mustParse(pin string) ssh.PublicKey {
	k, err := ssh.ParsePublicKey(decodePin(pin))
	if err != nil {
		return zeroKey{}
	}
	return k
}

func decodePin(pin string) []byte {
	for i := 0; i < len(pin); i++ {
		if pin[i] == ' ' {
			b, _ := base64.StdEncoding.DecodeString(pin[i+1:])
			return b
		}
	}
	return nil
}

type zeroKey struct{}

func (zeroKey) Type() string                        { return "unknown" }
func (zeroKey) Marshal() []byte                     { return nil }
func (zeroKey) Verify([]byte, *ssh.Signature) error { return errors.New("unusable key") }

func isAlive(c *ssh.Client) bool {
	type result struct{ err error }
	ch := make(chan result, 1)
	go func() {
		_, _, err := c.SendRequest("keepalive@openssh.com", true, nil)
		ch <- result{err}
	}()
	select {
	case r := <-ch:
		return r.err == nil
	case <-time.After(5 * time.Second):
		return false
	}
}

func (p *Pool) keepalive(sl *slot, client *ssh.Client, stop chan struct{}) {
	ticker := time.NewTicker(keepaliveEvery)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				sl.mu.Lock()
				if sl.client == client {
					sl.teardownLocked()
					p.emit(sl.serverID, "disconnected", "keepalive failed: "+err.Error())
				}
				sl.mu.Unlock()
				return
			}
		}
	}
}

// teardownLocked closes the connection. Callers must hold sl.mu.
func (sl *slot) teardownLocked() {
	if sl.stopKeep != nil {
		close(sl.stopKeep)
		sl.stopKeep = nil
	}
	if sl.client != nil {
		_ = sl.client.Close()
		sl.client = nil
	}
	if sl.jumpLease != nil {
		sl.jumpLease.Release()
		sl.jumpLease = nil
	}
	sl.refs = 0
}

func (p *Pool) reapLoop() {
	if p.idleTTL <= 0 {
		return
	}
	ticker := time.NewTicker(reapEvery)
	defer ticker.Stop()
	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.mu.Lock()
			slots := make([]*slot, 0, len(p.slots))
			for _, sl := range p.slots {
				slots = append(slots, sl)
			}
			p.mu.Unlock()

			for _, sl := range slots {
				sl.mu.Lock()
				if sl.client != nil && sl.refs == 0 && time.Since(sl.lastUsed) > p.idleTTL {
					sl.teardownLocked()
					p.emit(sl.serverID, "disconnected", "idle timeout")
				}
				sl.mu.Unlock()
			}
		}
	}
}

// Disconnect force-closes a server connection regardless of outstanding leases.
func (p *Pool) Disconnect(serverID string) {
	sl := p.slotFor(serverID)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	if sl.client != nil {
		sl.teardownLocked()
		p.emit(serverID, "disconnected", "closed by user")
	}
}

// IsConnected reports whether a live connection is currently pooled.
func (p *Pool) IsConnected(serverID string) bool {
	sl := p.slotFor(serverID)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.client != nil
}

// ActiveRefs returns the number of outstanding leases for a server.
func (p *Pool) ActiveRefs(serverID string) int {
	sl := p.slotFor(serverID)
	sl.mu.Lock()
	defer sl.mu.Unlock()
	return sl.refs
}

// Exec runs a single command and captures its output.
func (p *Pool) Exec(serverID, cmd string) (ExecResult, error) {
	l, err := p.Acquire(serverID)
	if err != nil {
		return ExecResult{}, err
	}
	defer l.Release()
	return runOn(l.Client, cmd)
}

func runOn(client *ssh.Client, cmd string) (ExecResult, error) {
	sess, err := client.NewSession()
	if err != nil {
		return ExecResult{}, err
	}
	defer sess.Close()

	var out, errBuf bytes.Buffer
	sess.Stdout = &out
	sess.Stderr = &errBuf

	err = sess.Run(cmd)
	res := ExecResult{Stdout: out.String(), Stderr: errBuf.String()}
	if err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			res.Code = exitErr.ExitStatus()
			return res, nil
		}
		return res, err
	}
	return res, nil
}

// Probe opens a connection, measures round-trip latency and collects a short
// host summary for the server detail panel.
type Probe struct {
	OK        bool   `json:"ok"`
	LatencyMS int64  `json:"latencyMs"`
	OS        string `json:"os"`
	Uptime    string `json:"uptime"`
	Load      string `json:"load"`
	HasTmux   bool   `json:"hasTmux"`
	TmuxVer   string `json:"tmuxVersion"`
	Error     string `json:"error"`
}

// TestConnection dials the server and reports basic facts about it.
func (p *Pool) TestConnection(serverID string) Probe {
	start := time.Now()
	l, err := p.Acquire(serverID)
	if err != nil {
		return Probe{OK: false, Error: err.Error()}
	}
	defer l.Release()
	latency := time.Since(start).Milliseconds()

	pr := Probe{OK: true, LatencyMS: latency}
	if r, err := runOn(l.Client, `uname -sr 2>/dev/null || echo unknown`); err == nil {
		pr.OS = trimLine(r.Stdout)
	}
	if r, err := runOn(l.Client, `uptime -p 2>/dev/null || uptime 2>/dev/null || echo ''`); err == nil {
		pr.Uptime = trimLine(r.Stdout)
	}
	if r, err := runOn(l.Client, `cat /proc/loadavg 2>/dev/null | cut -d' ' -f1-3 || echo ''`); err == nil {
		pr.Load = trimLine(r.Stdout)
	}
	if r, err := runOn(l.Client, `tmux -V 2>/dev/null || echo ''`); err == nil {
		pr.TmuxVer = trimLine(r.Stdout)
		pr.HasTmux = pr.TmuxVer != ""
	}
	return pr
}

func trimLine(s string) string {
	return string(bytes.TrimSpace([]byte(s)))
}
