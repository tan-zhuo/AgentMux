package app

import (
	"errors"
	"fmt"
	"strings"

	"agentmux/internal/sshx"
	"agentmux/internal/store"
	"agentmux/internal/tmuxx"
)

// TerminalService opens and drives remote PTYs.
type TerminalService struct{ core *Core }

// NewTerminalService binds a terminal service to the core.
func NewTerminalService(c *Core) *TerminalService { return &TerminalService{core: c} }

// ServiceName identifies the service in Wails logs.
func (t *TerminalService) ServiceName() string { return "TerminalService" }

// noShell is the answer for a host that was added for its screen. The UI does
// not offer a terminal on one, so reaching this means something asked anyway —
// a restored tab, a command palette entry, an older frontend — and a sentence
// beats an SSH failure about a missing agent socket, which describes the wrong
// problem entirely.
func (t *TerminalService) noShell(serverID string) error {
	if t.core.Store.ServerKindOf(serverID) != store.KindDesktop {
		return nil
	}
	return fmt.Errorf("this host was added for its desktop; there is no shell on it")
}

// OpenShell starts a plain login shell on a server. This is the escape hatch
// that keeps full SSH available: anything the user could type over ssh, they can
// type here.
func (t *TerminalService) OpenShell(serverID string, cols, rows int) (sshx.ShellInfo, error) {
	if err := t.noShell(serverID); err != nil {
		return sshx.ShellInfo{}, err
	}
	return t.core.Shells.Open(sshx.ShellOptions{
		ServerID: serverID, Cols: cols, Rows: rows,
		WindowsHost: t.core.IsSSHWin(serverID),
	})
}

// OpenCommand starts a PTY running one command instead of a login shell. It
// backs the install flow on servers that do not have tmux yet, where there is
// no session to put the work into.
func (t *TerminalService) OpenCommand(serverID, command string, cols, rows int) (sshx.ShellInfo, error) {
	if err := t.noShell(serverID); err != nil {
		return sshx.ShellInfo{}, err
	}
	if strings.TrimSpace(command) == "" {
		return sshx.ShellInfo{}, errors.New("command is required")
	}
	return t.core.Shells.Open(sshx.ShellOptions{
		ServerID: serverID, Cols: cols, Rows: rows, Command: command,
		WindowsHost: t.core.IsSSHWin(serverID),
		OneShot:     true,
	})
}

// OpenWorkspace starts a login shell already sitting in the workspace directory
// with the workspace environment applied.
func (t *TerminalService) OpenWorkspace(workspaceID string, cols, rows int) (sshx.ShellInfo, error) {
	ws, err := t.core.Store.GetWorkspace(workspaceID)
	if err != nil {
		return sshx.ShellInfo{}, err
	}
	return t.core.Shells.Open(sshx.ShellOptions{
		ServerID:    ws.ServerID,
		Cols:        cols,
		Rows:        rows,
		Cwd:         ws.RemotePath,
		Env:         ws.Env,
		WindowsHost: t.core.IsSSHWin(ws.ServerID),
	})
}

// AttachTmux attaches a PTY to an existing tmux session. Closing this terminal
// detaches; the session and everything in it keeps running on the server.
func (t *TerminalService) AttachTmux(serverID, session string, cols, rows int) (sshx.ShellInfo, error) {
	if err := t.noShell(serverID); err != nil {
		return sshx.ShellInfo{}, err
	}
	if session == "" {
		return sshx.ShellInfo{}, errors.New("session name is required")
	}
	exists, err := t.core.Tmux.HasSession(serverID, session)
	if err != nil {
		return sshx.ShellInfo{}, err
	}
	if !exists {
		return sshx.ShellInfo{}, fmt.Errorf("session %q no longer exists on this server", session)
	}
	return t.attachSession(serverID, session, cols, rows)
}

// attachSession connects a terminal to a persistent session by whatever means
// the host has: a tmux client over the host's transport, or — on a Windows
// host, where the sessions live in AgentMux's own daemon — a direct attachment
// adopted into the same shell manager. For a remote Windows host that
// attachment rides an SSH port forward; either way, closing the terminal
// detaches and the session keeps running.
func (t *TerminalService) attachSession(serverID, session string, cols, rows int) (sshx.ShellInfo, error) {
	if t.core.IsWinHost(serverID) {
		// Attaching again is all a dropped connection needs here — the session
		// itself lives in the daemon on the far side, not in this attachment —
		// so hand the shell manager the means to do it.
		reattach := func(o sshx.ShellOptions) (sshx.Opened, error) {
			return t.core.NatMuxFor(serverID).OpenAttach(session, o.Cols, o.Rows)
		}
		opened, err := reattach(sshx.ShellOptions{Cols: cols, Rows: rows})
		if err != nil {
			return sshx.ShellInfo{}, err
		}
		return t.core.Shells.AdoptWith(
			sshx.ShellOptions{ServerID: serverID, Cols: cols, Rows: rows}, opened, reattach), nil
	}
	return t.core.Shells.Open(sshx.ShellOptions{
		ServerID: serverID,
		Cols:     cols,
		Rows:     rows,
		Command:  tmuxx.AttachCommand(session),
	})
}

// AttachAgent attaches to the tmux session backing an agent, creating it first
// if the session vanished.
func (t *TerminalService) AttachAgent(agentID string, cols, rows int) (sshx.ShellInfo, error) {
	ag, err := t.core.Store.GetAgent(agentID)
	if err != nil {
		return sshx.ShellInfo{}, err
	}
	ws, err := t.core.Store.GetWorkspace(ag.WorkspaceID)
	if err != nil {
		return sshx.ShellInfo{}, err
	}
	exists, err := t.core.Tmux.HasSession(ws.ServerID, ag.TmuxSession)
	if err != nil {
		return sshx.ShellInfo{}, err
	}
	if !exists {
		if err := t.core.Tmux.NewSession(ws.ServerID, ag.TmuxSession, ws.RemotePath); err != nil {
			return sshx.ShellInfo{}, err
		}
	}
	return t.attachSession(ws.ServerID, ag.TmuxSession, cols, rows)
}

// Write forwards keystrokes to a PTY. Payload is base64 so binary and partial
// UTF-8 sequences survive the JSON hop.
func (t *TerminalService) Write(id, b64 string) error { return t.core.Shells.Write(id, b64) }

// WriteSeq is Write with exactly-once delivery per (writer, seq) — what the
// retrying send queue on a weak link needs. Plain Write stays for pages from
// before it existed.
func (t *TerminalService) WriteSeq(id, writer string, seq int64, b64 string) error {
	return t.core.Shells.WriteSeq(id, writer, seq, b64)
}

// Resize propagates a terminal resize.
func (t *TerminalService) Resize(id string, cols, rows int) error {
	return t.core.Shells.Resize(id, cols, rows)
}

// Scrollback returns buffered output so a re-mounted terminal can replay it.
func (t *TerminalService) Scrollback(id string) (string, error) {
	return t.core.Shells.Scrollback(id)
}

// Close ends a PTY.
func (t *TerminalService) Close(id string) error { return t.core.Shells.Close(id) }

// List returns every open PTY.
func (t *TerminalService) List() []sshx.ShellInfo { return t.core.Shells.List() }

// LoadTabs restores the persisted terminal layout.
func (t *TerminalService) LoadTabs() ([]store.TerminalTab, error) { return t.core.Store.ListTabs() }

// SaveTabs persists the terminal layout so it survives a restart.
func (t *TerminalService) SaveTabs(tabs []store.TerminalTab) error {
	return t.core.Store.ReplaceTabs(tabs)
}
