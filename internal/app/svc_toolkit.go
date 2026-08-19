package app

import (
	"fmt"
	"strings"

	"agentmux/internal/agentkit"
	"agentmux/internal/sshx"
	"agentmux/internal/tmuxx"
)

// installSession is the tmux session AgentMux runs installs in, one per server.
// Keeping installs in tmux means a dropped connection cannot abandon a
// half-written package tree, and the user can answer a sudo prompt by attaching.
const installSession = "agentmux/install"

// ToolkitService detects and installs the agent CLIs a server needs.
type ToolkitService struct{ core *Core }

// NewToolkitService binds a toolkit service to the core.
func NewToolkitService(c *Core) *ToolkitService { return &ToolkitService{core: c} }

// ServiceName identifies the service in Wails logs.
func (t *ToolkitService) ServiceName() string { return "ToolkitService" }

// Catalog returns every installable tool, runtimes first.
func (t *ToolkitService) Catalog() []agentkit.Tool { return agentkit.All() }

// Detect reports what is already installed on a server, probing in whichever
// shell dialect the host actually speaks.
func (t *ToolkitService) Detect(serverID string) agentkit.Report {
	return t.detect(serverID)
}

func (t *ToolkitService) detect(serverID string) agentkit.Report {
	if t.core.IsWinHost(serverID) {
		return agentkit.DetectWindows(t.core.Run, serverID)
	}
	return agentkit.Detect(t.core.Run, serverID)
}

// AgentChoice is one runnable agent CLI found on a server.
type AgentChoice struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Command string `json:"command"`
	Version string `json:"version"`
}

// InstalledAgents lists the agent CLIs a server can actually run right now.
// The quick-launch picker needs only this, so it avoids paying for the full
// report every time someone opens the menu.
func (t *ToolkitService) InstalledAgents(serverID string) []AgentChoice {
	rep := t.detect(serverID)
	out := []AgentChoice{}
	for _, a := range rep.Agents {
		if !a.Installed {
			continue
		}
		out = append(out, AgentChoice{
			ID:      a.Tool.ID,
			Name:    a.Tool.Name,
			Command: a.Tool.RunCommand,
			Version: a.Version,
		})
	}
	return out
}

// InstallStarted tells the frontend where to watch the install run.
type InstallStarted struct {
	ServerID string `json:"serverId"`
	ToolID   string `json:"toolId"`
	ToolName string `json:"toolName"`
	MethodID string `json:"methodId"`
	Script   string `json:"script"`
	// Session is the tmux session the install runs in, empty when tmux is not
	// installed yet and the install has to run in a plain PTY.
	Session   string `json:"session"`
	UsesTmux  bool   `json:"usesTmux"`
	NeedsRoot bool   `json:"needsRoot"`
	// Command is what the frontend should run in a PTY when UsesTmux is false.
	Command string `json:"command"`
}

// Install runs the chosen install method for a tool. The wrapped script — PATH
// prelude, npm permission and mirror fallbacks, a success/failure banner — runs
// as its own window in the install session, so nothing a previous install left
// in a pane can swallow it. When tmux itself is missing — the bootstrap case —
// the caller opens a plain PTY running the same script.
func (t *ToolkitService) Install(serverID, toolID, methodID string) (InstallStarted, error) {
	tool, ok := agentkit.ByID(toolID)
	if !ok {
		return InstallStarted{}, fmt.Errorf("unknown tool %q", toolID)
	}
	method, ok := agentkit.MethodByID(tool, methodID)
	if !ok {
		return InstallStarted{}, fmt.Errorf("%s has no install method %q", tool.Name, methodID)
	}

	started := InstallStarted{
		ServerID:  serverID,
		ToolID:    tool.ID,
		ToolName:  tool.Name,
		MethodID:  method.ID,
		Script:    method.Script,
		NeedsRoot: method.NeedsRoot,
	}
	if t.core.IsWinHost(serverID) {
		banner := fmt.Sprintf("echo '=== AgentMux: installing %s via %s ==='", tool.Name, method.Label)
		return t.startTyped(serverID, banner+"; "+method.Script, started)
	}
	full := agentkit.MethodScript(tool.Name+" via "+method.Label, method)
	return t.start(serverID, tool.ID, full, started)
}

// InstallCustom runs a user-supplied script the same way, for tools that are not
// in the catalogue.
func (t *ToolkitService) InstallCustom(serverID, label, script string) (InstallStarted, error) {
	if strings.TrimSpace(script) == "" {
		return InstallStarted{}, fmt.Errorf("script is empty")
	}
	if label == "" {
		label = "custom script"
	}
	started := InstallStarted{ServerID: serverID, ToolID: "custom", ToolName: label, Script: script}
	if t.core.IsWinHost(serverID) {
		return t.startTyped(serverID, fmt.Sprintf("echo '=== AgentMux: %s ==='; %s", label, script), started)
	}
	full := agentkit.InstallScript(label, script)
	return t.start(serverID, "custom", full, started)
}

// start places a wrapped install script in a fresh window of the install
// session, or hands it back for a plain PTY when tmux is not there yet. A
// fresh window means whatever a previous install left in a pane — an open
// pager, an unanswered sudo prompt — cannot swallow the command, which the old
// type-into-the-active-pane approach silently did.
//
// This talks to tmux directly rather than through the Mux interface: windows
// are a tmux concept, and this path is only taken for hosts tmux serves.
func (t *ToolkitService) start(serverID, window, script string, started InstallStarted) (InstallStarted, error) {
	tm := tmuxx.New(t.core.Run)
	if info := tm.Available(serverID); info.Available {
		if err := tm.NewWindowExec(serverID, installSession, window, script); err != nil {
			return started, err
		}
		started.Session = installSession
		started.UsesTmux = true
		return started, nil
	}
	started.Command = script
	return started, nil
}

// startTyped is the Windows-host path: the session daemon has sessions but no
// windows, and its shell is PowerShell, which the wrapped POSIX script would
// not survive — so the raw command is typed into the shared install session as
// before.
func (t *ToolkitService) startTyped(serverID, command string, started InstallStarted) (InstallStarted, error) {
	if info := t.core.Tmux.Available(serverID); info.Available {
		exists, err := t.core.Tmux.HasSession(serverID, installSession)
		if err != nil {
			return started, err
		}
		if !exists {
			if err := t.core.Tmux.NewSession(serverID, installSession, ""); err != nil {
				return started, err
			}
		}
		if err := t.core.Tmux.SendText(serverID, tmuxx.Target(installSession), command, true); err != nil {
			return started, err
		}
		started.Session = installSession
		started.UsesTmux = true
		return started, nil
	}
	started.Command = command
	return started, nil
}

// Verify re-probes a single tool after an install finishes.
func (t *ToolkitService) Verify(serverID, toolID string) (agentkit.Presence, error) {
	tool, ok := agentkit.ByID(toolID)
	if !ok {
		return agentkit.Presence{}, fmt.Errorf("unknown tool %q", toolID)
	}
	if t.core.IsWinHost(serverID) {
		return agentkit.VerifyWindows(t.core.Run, serverID, tool)
	}
	// Two things are needed to see a freshly installed binary. A login shell,
	// because installers write their PATH line into a profile that an SSH exec
	// never reads; and the explicit search path, because the profile line is
	// often guarded behind an interactivity check that a login shell still does
	// not satisfy.
	cmd := fmt.Sprintf(
		`bash -lc %s`,
		sshx.ShellQuote(agentkit.PathPrelude()+fmt.Sprintf(
			`p=$(command -v %s 2>/dev/null) && printf '%%s\n%%s\n' "$p" "$(%s %s 2>/dev/null | head -1)"`,
			sshx.ShellQuote(tool.Binary), tool.Binary, tool.VersionArgs)),
	)
	res, err := t.core.Run.Exec(serverID, cmd)
	if err != nil {
		return agentkit.Presence{Binary: tool.Binary}, err
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(res.Stdout, "\r\n", "\n")), "\n")
	if res.Code != 0 || len(lines) == 0 || lines[0] == "" {
		return agentkit.Presence{Binary: tool.Binary}, nil
	}
	p := agentkit.Presence{Binary: tool.Binary, Installed: true, Path: strings.TrimSpace(lines[0])}
	if len(lines) > 1 {
		p.Version = strings.TrimSpace(lines[1])
	}
	return p, nil
}

// InstallSessionName exposes the shared install session name to the frontend.
func (t *ToolkitService) InstallSessionName() string { return installSession }
