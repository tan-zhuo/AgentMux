package app

import (
	"os"
	"strings"

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
	return s.core.Pool.TestConnection(id)
}

// Connect warms a pooled connection without opening a terminal. This computer has
// nothing to warm, so it reports what it would report after a successful dial:
// that it is ready.
func (s *ServerService) Connect(id string) error {
	if s.core.IsLocal(id) {
		return s.core.Local.Available()
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
		if srv.Kind == store.KindLocal {
			// Nothing to connect and nothing to lose: this computer is either
			// usable or the platform cannot host anything, which Test explains.
			out = append(out, ConnStatus{
				ServerID:  srv.ID,
				Connected: s.core.Local.Available() == nil,
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
