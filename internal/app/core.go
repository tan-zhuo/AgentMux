// Package app wires the store, SSH pool, tmux client and PTY manager together
// and exposes them to the frontend as Wails services.
package app

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"agentmux/internal/sshx"
	"agentmux/internal/store"
	"agentmux/internal/tmuxx"
)

// idleTTL closes SSH connections that nothing has used for this long. It is the
// mechanism that keeps a hundred configured servers from meaning a hundred live
// sockets; anything a user is actually watching holds a lease and is exempt.
const idleTTL = 10 * time.Minute

// Core holds the shared, long-lived state of the application.
type Core struct {
	Store  *store.Store
	Pool   *sshx.Pool
	Tmux   *tmuxx.Client
	Shells *sshx.ShellManager

	emitMu sync.RWMutex
	emitFn func(name string, data any)

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewCore opens the database and builds the connection machinery.
func NewCore() (*Core, error) {
	st, err := store.Open()
	if err != nil {
		return nil, err
	}

	c := &Core{Store: st, stopCh: make(chan struct{})}
	c.Pool = sshx.NewPool(c, idleTTL, func(s sshx.ConnState) {
		c.Emit("server:state", s)
	})
	c.Tmux = tmuxx.New(c.Pool)
	c.Shells = sshx.NewShellManager(c.Pool, c.Emit)
	return c, nil
}

// SetEmitter installs the Wails event emitter once the application exists.
func (c *Core) SetEmitter(fn func(name string, data any)) {
	c.emitMu.Lock()
	c.emitFn = fn
	c.emitMu.Unlock()
}

// Emit publishes an event to the frontend, dropping it if the UI is not up yet.
func (c *Core) Emit(name string, data any) {
	c.emitMu.RLock()
	fn := c.emitFn
	c.emitMu.RUnlock()
	if fn != nil {
		fn(name, data)
	}
}

// Shutdown tears everything down in dependency order.
func (c *Core) Shutdown() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.Shells.CloseAll()
		c.Pool.Stop()
		_ = c.Store.Close()
	})
}

// --- sshx.Resolver ----------------------------------------------------------

// Resolve turns a server id into connection credentials for the pool.
func (c *Core) Resolve(serverID string) (sshx.Target, error) {
	s, err := c.Store.GetServer(serverID)
	if err != nil {
		return sshx.Target{}, err
	}
	password, passphrase, err := c.Store.Secrets(serverID)
	if err != nil {
		return sshx.Target{}, err
	}
	t := sshx.Target{
		ServerID:   s.ID,
		Name:       s.Name,
		Host:       s.Host,
		Port:       s.Port,
		User:       s.Username,
		AuthType:   string(s.AuthType),
		KeyPath:    s.KeyPath,
		Password:   password,
		Passphrase: passphrase,
		HostKey:    s.HostKey,
	}
	if s.JumpServerID != nil {
		t.JumpServerID = *s.JumpServerID
	}
	return t, nil
}

// PinHostKey stores a trust-on-first-use host key.
func (c *Core) PinHostKey(serverID, hostKey string) error {
	return c.Store.PinHostKey(serverID, hostKey)
}

// MarkOK records a successful connection.
func (c *Core) MarkOK(serverID string) {
	_ = c.Store.MarkServerOK(serverID)
}

// --- helpers ----------------------------------------------------------------

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Slug normalises a name into something safe for a tmux session path segment.
func Slug(s string) string {
	out := slugRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	out = strings.Trim(out, "-")
	if out == "" {
		out = "untitled"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

// DefaultSessionName builds the conventional tmux session name for an agent.
// tmux treats ':' and '.' as target separators, so only '/' is used to nest.
func DefaultSessionName(projectName, agentName string) string {
	return fmt.Sprintf("agentmux/%s/%s", Slug(projectName), Slug(agentName))
}

// shellCommands are the process names that mean "the pane is sitting at a
// prompt" rather than "an agent is working".
var shellCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
	"ksh": true, "csh": true, "tcsh": true, "ash": true, "login": true,
}
