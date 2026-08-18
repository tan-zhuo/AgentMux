package app

import (
	"os"
	"runtime"
	"strings"
	"time"

	"agentmux/internal/sshx"
	"agentmux/internal/store"
)

// ServerService exposes server CRUD and connection control to the frontend.
type ServerService struct{ core *Core }

// NewServerService binds a server service to the core.
func NewServerService(c *Core) *ServerService { return &ServerService{core: c} }

// ServiceName identifies the service in Wails logs.
func (s *ServerService) ServiceName() string { return "ServerService" }

// List returns every configured server.
func (s *ServerService) List() ([]store.Server, error) {
	return s.core.Store.ListServers()
}

// Get returns one server.
func (s *ServerService) Get(id string) (store.Server, error) {
	return s.core.Store.GetServer(id)
}

// Save creates or updates a server. Secrets are encrypted before they hit disk.
func (s *ServerService) Save(in store.ServerInput) (store.Server, error) {
	srv, err := s.core.Store.SaveServer(in)
	if err != nil {
		return store.Server{}, err
	}
	// Credentials may have changed; drop any pooled connection so the next use
	// re-authenticates rather than silently keeping the old session alive.
	if in.ID != "" {
		s.core.Pool.Disconnect(srv.ID)
	}
	return srv, nil
}

// Delete removes a server and disconnects it.
func (s *ServerService) Delete(id string) error {
	s.core.Pool.Disconnect(id)
	return s.core.Store.DeleteServer(id)
}

// Test reports latency plus a short host summary. For this computer that is a
// handful of local commands rather than a dial, but it answers the same question:
// can work be done here, and what is here.
func (s *ServerService) Test(id string) sshx.Probe {
	if s.core.IsLocal(id) {
		return s.core.Local.Probe()
	}
	if s.core.IsLocalWin(id) {
		return s.core.NativeLocal.Probe()
	}
	if s.core.IsSSHWin(id) {
		return s.winProbe(id)
	}
	return s.core.Pool.TestConnection(id)
}

// winProbe asks a remote Windows host the questions TestConnection asks of a
// POSIX one, in the dialect it actually speaks.
func (s *ServerService) winProbe(id string) sshx.Probe {
	start := time.Now()
	res, err := s.core.Run.Exec(id, `(Get-CimInstance Win32_OperatingSystem).Caption`)
	if err != nil {
		return sshx.Probe{Error: err.Error()}
	}
	p := sshx.Probe{OK: true, OS: strings.TrimSpace(res.Stdout)}
	if res, err := s.core.Run.Exec(id,
		`$b=(Get-CimInstance Win32_OperatingSystem).LastBootUpTime; `+
			`'{0:%d}d {0:hh}h {0:mm}m' -f ((Get-Date)-$b)`); err == nil {
		p.Uptime = strings.TrimSpace(res.Stdout)
	}
	// Sessions on this host persist through AgentMux's own daemon; for the one
	// question the flag answers — does work survive the window closing — yes.
	p.HasTmux = true
	p.TmuxVer = "AgentMux sessions"
	p.LatencyMS = time.Since(start).Milliseconds()
	return p
}

// Connect warms a pooled connection without opening a terminal. This computer has
// nothing to warm, so it reports what it would report after a successful dial:
// that it is ready.
func (s *ServerService) Connect(id string) error {
	if s.core.IsLocal(id) {
		return s.core.Local.Available()
	}
	if s.core.IsLocalWin(id) {
		return s.core.NativeLocal.Available()
	}
	lease, err := s.core.Pool.Acquire(id)
	if err != nil {
		return err
	}
	lease.Release()
	return nil
}

// LocalHost describes whether this computer can be managed, for the UI to show
// before it offers to add it.
type LocalHost struct {
	// Supported is false when this platform cannot host agents at all.
	Supported bool `json:"supported"`
	// Reason explains an unsupported platform, in a sentence a person can act on.
	Reason string `json:"reason"`
	// Name is the suggested display name: the machine's own hostname.
	Name string `json:"name"`
	// ExistingID is set when this computer is already in the tree, so the UI can
	// point at it instead of offering to add a second one.
	ExistingID string `json:"existingId"`
}

// LocalSupport reports whether this computer can be added as a host.
func (s *ServerService) LocalSupport() LocalHost {
	out := LocalHost{Name: localHostName()}
	if servers, err := s.core.Store.ListServers(); err == nil {
		for _, srv := range servers {
			if srv.Kind == store.KindLocal {
				out.ExistingID = srv.ID
				break
			}
		}
	}
	if err := s.core.Local.Available(); err != nil {
		out.Reason = err.Error()
		return out
	}
	out.Supported = true
	return out
}

// AddLocal puts this computer in the tree as a host.
//
// There is nothing to ask for — no address, no account, no key — so this takes no
// arguments beyond a name, and refuses rather than creating a second local host,
// because there is only one machine here.
func (s *ServerService) AddLocal(name string) (store.Server, error) {
	if err := s.core.Local.Available(); err != nil {
		return store.Server{}, err
	}
	servers, err := s.core.Store.ListServers()
	if err != nil {
		return store.Server{}, err
	}
	for _, srv := range servers {
		if srv.Kind == store.KindLocal {
			return srv, nil
		}
	}
	if strings.TrimSpace(name) == "" {
		name = localHostName()
	}
	return s.core.Store.SaveServer(store.ServerInput{
		Kind: store.KindLocal,
		Name: name,
		Tags: []string{"local"},
		// Deliberately not trusted. The orchestrator asks before acting here for
		// the same reason it asks anywhere else, and this happens to be the
		// machine with the user's own files on it.
		TrustLevel: store.TrustNormal,
	})
}

// LocalWinSupport reports whether this computer's native Windows side can be
// added as its own host. Everywhere but Windows the answer is no with a reason;
// on Windows it is the door to the work WSL cannot do — MSVC, WPF, running the
// .exe that was just built.
func (s *ServerService) LocalWinSupport() LocalHost {
	out := LocalHost{Name: localHostName() + " (Windows)"}
	if servers, err := s.core.Store.ListServers(); err == nil {
		for _, srv := range servers {
			if srv.Kind == store.KindLocalWin {
				out.ExistingID = srv.ID
				break
			}
		}
	}
	if err := s.core.NativeLocal.Available(); err != nil {
		out.Reason = err.Error()
		return out
	}
	out.Supported = true
	return out
}

// AddLocalWin puts this computer's native Windows side in the tree as a host.
// Like AddLocal it takes nothing but a name and refuses to create a second one.
func (s *ServerService) AddLocalWin(name string) (store.Server, error) {
	if err := s.core.NativeLocal.Available(); err != nil {
		return store.Server{}, err
	}
	servers, err := s.core.Store.ListServers()
	if err != nil {
		return store.Server{}, err
	}
	for _, srv := range servers {
		if srv.Kind == store.KindLocalWin {
			return srv, nil
		}
	}
	if strings.TrimSpace(name) == "" {
		name = localHostName() + " (Windows)"
	}
	return s.core.Store.SaveServer(store.ServerInput{
		Kind: store.KindLocalWin,
		Name: name,
		Tags: []string{"local", "windows"},
		// Deliberately not trusted, for the same reason the WSL host is not: this
		// is the machine with the user's own files on it.
		TrustLevel: store.TrustNormal,
	})
}

// Platform names the operating system this build runs on, so the UI can offer
// only the local host flavours that exist here — the native Windows host being
// the one that exists nowhere else.
func (s *ServerService) Platform() string { return runtime.GOOS }

// localHostName is what to call this computer in the tree.
func localHostName() string {
	if h, err := os.Hostname(); err == nil && strings.TrimSpace(h) != "" {
		return h
	}
	return "This computer"
}

// Disconnect force-closes a pooled connection.
func (s *ServerService) Disconnect(id string) { s.core.Pool.Disconnect(id) }

// ConnStatus is the live connection view for one server.
type ConnStatus struct {
	ServerID  string `json:"serverId"`
	Connected bool   `json:"connected"`
	Leases    int    `json:"leases"`
}

// Connections returns the live connection state of every configured server.
func (s *ServerService) Connections() ([]ConnStatus, error) {
	servers, err := s.core.Store.ListServers()
	if err != nil {
		return nil, err
	}
	out := make([]ConnStatus, 0, len(servers))
	for _, srv := range servers {
		if srv.Kind == store.KindLocal || srv.Kind == store.KindLocalWin {
			// Nothing to connect and nothing to lose: this computer is either
			// usable or the platform cannot host anything, which Test explains.
			available := s.core.Local.Available()
			if srv.Kind == store.KindLocalWin {
				available = s.core.NativeLocal.Available()
			}
			out = append(out, ConnStatus{
				ServerID:  srv.ID,
				Connected: available == nil,
			})
			continue
		}
		out = append(out, ConnStatus{
			ServerID:  srv.ID,
			Connected: s.core.Pool.IsConnected(srv.ID),
			Leases:    s.core.Pool.ActiveRefs(srv.ID),
		})
	}
	return out, nil
}

// ClearHostKey forgets the pinned host key so the next connection re-pins. Used
// after a legitimate server key rotation.
func (s *ServerService) ClearHostKey(id string) error {
	s.core.Pool.Disconnect(id)
	return s.core.Store.PinHostKey(id, "")
}

// Version is the build's identity. Release builds set it with
// -ldflags "-X agentmux/internal/app.Version=v1.2.3"; a local build says so.
var Version = "dev"

// Diagnostics reports facts the UI shows in the settings panel.
type Diagnostics struct {
	Version       string `json:"version"`
	DataDir       string `json:"dataDir"`
	KeyInFile     bool   `json:"keyInFile"`
	KeyLocationOK bool   `json:"keyLocationOk"`
}

// Diagnostics describes where AgentMux keeps its data and how well protected
// the master key is.
func (s *ServerService) Diagnostics() Diagnostics {
	return Diagnostics{
		Version:       Version,
		DataDir:       s.core.Store.Dir,
		KeyInFile:     s.core.Store.KeyInFile,
		KeyLocationOK: !s.core.Store.KeyInFile,
	}
}
