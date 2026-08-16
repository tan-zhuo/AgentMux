package app

import (
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

// Test dials the server and reports latency plus a short host summary.
func (s *ServerService) Test(id string) sshx.Probe {
	return s.core.Pool.TestConnection(id)
}

// Connect warms a pooled connection without opening a terminal.
func (s *ServerService) Connect(id string) error {
	lease, err := s.core.Pool.Acquire(id)
	if err != nil {
		return err
	}
	lease.Release()
	return nil
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
