package app

import (
	"agentmux/internal/tmuxx"
)

// TmuxService exposes remote tmux inspection and control.
type TmuxService struct{ core *Core }

// NewTmuxService binds a tmux service to the core.
func NewTmuxService(c *Core) *TmuxService { return &TmuxService{core: c} }

// ServiceName identifies the service in Wails logs.
func (t *TmuxService) ServiceName() string { return "TmuxService" }

// Info reports whether tmux is usable on a server.
func (t *TmuxService) Info(serverID string) tmuxx.Info { return t.core.Tmux.Available(serverID) }

// ServerView is everything the tmux panel needs for one server, fetched in two
// round trips rather than one per session.
type ServerView struct {
	ServerID string          `json:"serverId"`
	Info     tmuxx.Info      `json:"info"`
	Sessions []tmuxx.Session `json:"sessions"`
	Panes    []tmuxx.Pane    `json:"panes"`
	Error    string          `json:"error"`
}

// View returns the tmux state of one server.
func (t *TmuxService) View(serverID string) ServerView {
	v := ServerView{ServerID: serverID}
	v.Info = t.core.Tmux.Available(serverID)
	if !v.Info.Available {
		v.Error = v.Info.Error
		v.Sessions = []tmuxx.Session{}
		v.Panes = []tmuxx.Pane{}
		return v
	}
	sessions, err := t.core.Tmux.ListSessions(serverID)
	if err != nil {
		v.Error = err.Error()
		v.Sessions = []tmuxx.Session{}
		v.Panes = []tmuxx.Pane{}
		return v
	}
	panes, err := t.core.Tmux.ListPanes(serverID)
	if err != nil {
		v.Error = err.Error()
		panes = []tmuxx.Pane{}
	}
	v.Sessions = sessions
	v.Panes = panes
	return v
}

// Sessions lists tmux sessions on a server.
func (t *TmuxService) Sessions(serverID string) ([]tmuxx.Session, error) {
	return t.core.Tmux.ListSessions(serverID)
}

// Panes lists every pane on a server.
func (t *TmuxService) Panes(serverID string) ([]tmuxx.Pane, error) {
	return t.core.Tmux.ListPanes(serverID)
}

// CreateSession makes a new detached session.
func (t *TmuxService) CreateSession(serverID, name, cwd string) error {
	return t.core.Tmux.NewSession(serverID, name, cwd)
}

// KillSession terminates a session and everything inside it.
func (t *TmuxService) KillSession(serverID, name string) error {
	return t.core.Tmux.KillSession(serverID, name)
}

// RenameSession renames a session.
func (t *TmuxService) RenameSession(serverID, from, to string) error {
	return t.core.Tmux.RenameSession(serverID, from, to)
}

// SendText types literal text into a pane without interpreting key names.
func (t *TmuxService) SendText(serverID, target, text string, pressEnter bool) error {
	return t.core.Tmux.SendText(serverID, target, text, pressEnter)
}

// SendKey sends a named key such as "C-c" or "Escape".
func (t *TmuxService) SendKey(serverID, target, key string) error {
	return t.core.Tmux.SendKey(serverID, target, key)
}

// Capture returns the tail of a pane's scrollback without attaching to it.
func (t *TmuxService) Capture(serverID, target string, lines int) (string, error) {
	return t.core.Tmux.CapturePane(serverID, target, lines)
}
