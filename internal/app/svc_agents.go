package app

import (
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentmux/internal/sshx"
	"agentmux/internal/store"
	"agentmux/internal/tmuxx"
)

// AgentService owns the lifecycle of AI agents living inside remote tmux panes.
type AgentService struct {
	core *Core

	// lastActivity remembers what each agent was doing at the previous poll, so
	// attention marks are raised on transitions rather than levels: "started
	// asking" and "stopped working" are events, "is still asking" is not. Kept
	// in memory on purpose — after a restart the next poll re-raises anything
	// still pending, which is the right way to be wrong.
	actMu        sync.Mutex
	lastActivity map[string]store.AgentActivity
}

// NewAgentService binds an agent service to the core.
func NewAgentService(c *Core) *AgentService {
	return &AgentService{core: c, lastActivity: map[string]store.AgentActivity{}}
}

// ServiceName identifies the service in Wails logs.
func (a *AgentService) ServiceName() string { return "AgentService" }

// List returns every agent definition.
func (a *AgentService) List() ([]store.Agent, error) { return a.core.Store.ListAgents() }

// Store exposes the database to tests that need to set up fixtures against the
// same core the service uses. It is not part of the frontend surface: Wails
// binds exported methods, and this one returns a type it cannot marshal, so it
// is skipped by the binding generator.
func (a *AgentService) Store() *store.Store { return a.core.Store }

// Save creates or updates an agent. A blank tmux session name is filled in with
// the agentmux/{project}/{agent} convention, and a session still carrying that
// convention follows the agent when it is renamed — see sessionAfterRename.
func (a *AgentService) Save(ag store.Agent) (store.Agent, error) {
	if strings.TrimSpace(ag.TmuxSession) == "" {
		projectName, err := a.projectNameFor(ag.WorkspaceID)
		if err != nil {
			return store.Agent{}, err
		}
		ag.TmuxSession = DefaultSessionName(projectName, ag.Name)
	}
	if strings.ContainsAny(ag.TmuxSession, ":.") {
		return store.Agent{}, fmt.Errorf("tmux session name %q cannot contain ':' or '.'", ag.TmuxSession)
	}
	ag.TmuxSession = a.sessionAfterRename(ag)
	saved, err := a.core.Store.SaveAgent(ag)
	if err != nil {
		return store.Agent{}, err
	}
	a.publish()
	return saved, nil
}

// Delete removes an agent definition. The remote tmux session is left running
// on purpose — deleting a row in the desktop app must never kill remote work.
func (a *AgentService) Delete(id string) error {
	if err := a.core.Store.DeleteAgent(id); err != nil {
		return err
	}
	a.actMu.Lock()
	delete(a.lastActivity, id)
	a.actMu.Unlock()
	a.publish()
	return nil
}

// publish pushes the agent list to every window. Renaming an agent is a change
// only the window that did it would otherwise see: the poller compares runtime
// state, and a detached terminal never asks for the tree at all. Both keep
// showing the old name until something else happens to them.
func (a *AgentService) publish() {
	agents, err := a.core.Store.ListAgents()
	if err != nil {
		return
	}
	a.core.Emit("agents:updated", agents)
}

// sessionAfterRename keeps a conventional session name in step with the agent's
// name, and returns the session the save should store.
//
// Only a session the app named itself moves: one the user typed is theirs, and
// renaming the agent is not permission to rewrite it. A session that is running
// is renamed on the server first — the stored name is how the agent finds its
// pane again, so pointing it at a name that exists nowhere would strand the
// work. Anything that stops us from confirming the move (an unreachable host,
// no tmux, a rename that fails) keeps the old name, which is wrong on screen
// but still attached to the right session.
func (a *AgentService) sessionAfterRename(ag store.Agent) string {
	if ag.ID == "" {
		return ag.TmuxSession
	}
	prev, err := a.core.Store.GetAgent(ag.ID)
	if err != nil || prev.Name == ag.Name {
		return ag.TmuxSession
	}
	oldProject, err := a.projectNameFor(prev.WorkspaceID)
	if err != nil || prev.TmuxSession != DefaultSessionName(oldProject, prev.Name) {
		return ag.TmuxSession
	}
	newProject, err := a.projectNameFor(ag.WorkspaceID)
	if err != nil {
		return ag.TmuxSession
	}
	want := DefaultSessionName(newProject, ag.Name)
	if want == prev.TmuxSession || strings.ContainsAny(want, ":.") {
		return ag.TmuxSession
	}
	// The dialog either left the old session in the field or filled in the
	// conventional new one as the name was typed. Any third answer is a name
	// the user chose, and it wins.
	if ag.TmuxSession != prev.TmuxSession && ag.TmuxSession != want {
		return ag.TmuxSession
	}

	ws, err := a.core.Store.GetWorkspace(prev.WorkspaceID)
	if err != nil {
		return prev.TmuxSession
	}
	// An offline host is not asked, and nothing is stranded by that: whatever is
	// running there still answers to the old name, which is the name we keep.
	if !a.core.IsReachable(ws.ServerID) {
		return prev.TmuxSession
	}
	if info := a.core.Tmux.Available(ws.ServerID); !info.Available {
		return prev.TmuxSession
	}
	exists, err := a.core.Tmux.HasSession(ws.ServerID, prev.TmuxSession)
	if err != nil {
		return prev.TmuxSession
	}
	if exists {
		if err := a.core.Tmux.RenameSession(ws.ServerID, prev.TmuxSession, want); err != nil {
			return prev.TmuxSession
		}
	}
	return want
}

// SuggestSession returns the conventional session name for a workspace/agent
// pair so the UI can prefill the field.
func (a *AgentService) SuggestSession(workspaceID, agentName string) (string, error) {
	projectName, err := a.projectNameFor(workspaceID)
	if err != nil {
		return "", err
	}
	return DefaultSessionName(projectName, agentName), nil
}

func (a *AgentService) projectNameFor(workspaceID string) (string, error) {
	ws, err := a.core.Store.GetWorkspace(workspaceID)
	if err != nil {
		return "", err
	}
	projects, err := a.core.Store.ListProjects()
	if err != nil {
		return "", err
	}
	for _, p := range projects {
		if p.ID == ws.ProjectID {
			return p.Name, nil
		}
	}
	return ws.Name, nil
}

// StartResult reports what happened when an agent was asked to start.
type StartResult struct {
	AgentID string            `json:"agentId"`
	Status  store.AgentStatus `json:"status"`
	Session string            `json:"session"`
	Target  string            `json:"target"`
	Created bool              `json:"createdSession"`
	Skipped bool              `json:"alreadyRunning"`
	Error   string            `json:"error"`
}

// Start ensures the agent's tmux session exists and launches its command in it.
// Starting an already-running agent is a no-op rather than a double launch.
func (a *AgentService) Start(agentID string) (StartResult, error) {
	ag, ws, err := a.resolve(agentID)
	if err != nil {
		return StartResult{AgentID: agentID, Error: err.Error()}, err
	}
	res := StartResult{AgentID: agentID, Session: ag.TmuxSession}

	if info := a.core.Tmux.Available(ws.ServerID); !info.Available {
		err := fmt.Errorf("tmux is not available on this server: %s", info.Error)
		res.Error = err.Error()
		return res, err
	}

	exists, err := a.core.Tmux.HasSession(ws.ServerID, ag.TmuxSession)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	if !exists {
		if err := a.core.Tmux.NewSession(ws.ServerID, ag.TmuxSession, ws.RemotePath); err != nil {
			res.Error = err.Error()
			return res, err
		}
		res.Created = true
	}

	panes, err := a.core.Tmux.ListPanes(ws.ServerID)
	if err != nil {
		res.Error = err.Error()
		return res, err
	}
	pane, ok := pickPane(ag, panes)
	if !ok {
		err := fmt.Errorf("tmux session %q has no panes", ag.TmuxSession)
		res.Error = err.Error()
		return res, err
	}
	target := pane.PaneID
	res.Target = target
	_ = a.core.Store.SetAgentPane(ag.ID, ag.TmuxSession, pane.PaneID)

	if !shellCommands[pane.Command] {
		// Something is already occupying the pane; treat that as running so a
		// double-click never launches a second agent on top of the first.
		res.Skipped = true
		res.Status = store.StatusRunning
		_ = a.core.Store.UpdateAgentRuntime(ag.ID, store.StatusRunning, a.prevActivity(ag.ID), &pane.PID, "")
		a.emitAgents()
		return res, nil
	}

	cmd := a.launchCommand(ws, ag.Command)
	if err := a.core.Tmux.SendText(ws.ServerID, target, cmd, true); err != nil {
		res.Error = err.Error()
		return res, err
	}

	res.Status = store.StatusRunning
	_ = a.core.Store.UpdateAgentRuntime(ag.ID, store.StatusRunning, store.ActivityWorking, &pane.PID, "starting…")
	// Starting an agent is a person acting on it: whatever it wanted before is
	// superseded, and its first quiet moment after this launch is a fresh "done".
	a.noteEngaged(ag, store.ActivityWorking)
	a.emitAgents()
	return res, nil
}

// noteEngaged records that a person just acted on the agent: the activity
// baseline moves and any pending attention mark comes down.
func (a *AgentService) noteEngaged(ag store.Agent, act store.AgentActivity) {
	a.actMu.Lock()
	a.lastActivity[ag.ID] = act
	a.actMu.Unlock()
	_ = a.core.Store.SetAgentAttention(ag.ID, store.AttentionNone)
}

// launchCommand builds the line typed into the agent's session, in the dialect
// the host's shell actually speaks.
func (a *AgentService) launchCommand(ws store.Workspace, command string) string {
	if a.core.IsWinHost(ws.ServerID) {
		return buildLaunchCommandWin(ws, command)
	}
	return buildLaunchCommand(ws, command)
}

// buildLaunchCommand prefixes the agent command with its workspace environment
// and working directory so a pre-existing session still lands in the right place.
func buildLaunchCommand(ws store.Workspace, command string) string {
	var b strings.Builder
	keys := make([]string, 0, len(ws.Env))
	for k := range ws.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("export " + k + "=" + sshx.ShellQuote(ws.Env[k]) + "; ")
	}
	if ws.RemotePath != "" {
		b.WriteString("cd " + sshx.ShellQuote(ws.RemotePath) + " && ")
	}
	b.WriteString(command)
	return b.String()
}

// Stop sends Ctrl-C to the agent's pane, letting the agent shut down cleanly
// while leaving the tmux session and its scrollback intact.
func (a *AgentService) Stop(agentID string) error {
	ag, ws, err := a.resolve(agentID)
	if err != nil {
		return err
	}
	target, err := a.targetFor(ag, ws.ServerID)
	if err != nil {
		return err
	}
	if err := a.core.Tmux.SendKey(ws.ServerID, target, "C-c"); err != nil {
		return err
	}
	_ = a.core.Store.UpdateAgentRuntime(ag.ID, store.StatusIdle, store.ActivityNone, nil, "")
	// A deliberate stop is not a result to review — no done-mark for it.
	a.noteEngaged(ag, store.ActivityNone)
	a.emitAgents()
	return nil
}

// Kill destroys the agent's tmux session outright. This is the only operation
// that loses remote scrollback, so the UI must confirm it.
func (a *AgentService) Kill(agentID string) error {
	ag, ws, err := a.resolve(agentID)
	if err != nil {
		return err
	}
	if err := a.core.Tmux.KillSession(ws.ServerID, ag.TmuxSession); err != nil {
		return err
	}
	_ = a.core.Store.UpdateAgentRuntime(ag.ID, store.StatusDetached, store.ActivityNone, nil, "")
	a.noteEngaged(ag, store.ActivityNone)
	a.emitAgents()
	return nil
}

// Restart stops the agent, waits for it to settle, then starts it again.
func (a *AgentService) Restart(agentID string) (StartResult, error) {
	if err := a.Stop(agentID); err != nil {
		return StartResult{AgentID: agentID, Error: err.Error()}, err
	}
	time.Sleep(1200 * time.Millisecond)
	return a.Start(agentID)
}

// Receipt is the per-agent outcome of a message or broadcast.
type Receipt struct {
	AgentID   string `json:"agentId"`
	AgentName string `json:"agentName"`
	OK        bool   `json:"ok"`
	Target    string `json:"target"`
	Error     string `json:"error"`
	At        int64  `json:"at"`
}

// Send types a message into an agent's pane. With execute false the text is
// typed but not submitted, which lets the user review it before pressing Enter.
func (a *AgentService) Send(agentID, message string, execute bool) Receipt {
	r := Receipt{AgentID: agentID, At: time.Now().Unix()}
	ag, ws, err := a.resolve(agentID)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.AgentName = ag.Name

	target, err := a.targetFor(ag, ws.ServerID)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Target = target

	if err := a.core.Tmux.SendText(ws.ServerID, target, message, execute); err != nil {
		r.Error = err.Error()
		return r
	}
	r.OK = true
	// Messaging an agent answers whatever it was flagged for. A submitted
	// message puts it back to work; one typed for review leaves the baseline
	// where the next poll will read it.
	if execute {
		a.noteEngaged(ag, store.ActivityWorking)
	} else {
		a.noteEngaged(ag, a.prevActivity(ag.ID))
	}
	a.emitAgents()
	return r
}

// BroadcastTarget names one recipient. It is either a registered agent, or a
// tmux session addressed directly.
//
// Sessions matter as much as agents: most people start work by launching into a
// directory, which produces a running tmux session and no agent record at all.
// Restricting a broadcast to registered agents made the feature useless for
// exactly the setup it was built for.
type BroadcastTarget struct {
	AgentID  string `json:"agentId"`
	ServerID string `json:"serverId"`
	Session  string `json:"session"`
}

// SendToSession types a message into a tmux session that has no agent record.
func (a *AgentService) SendToSession(serverID, session, message string, execute bool) Receipt {
	r := Receipt{AgentName: session, Target: session, At: time.Now().Unix()}
	if serverID == "" || session == "" {
		r.Error = "a server and a session are required"
		return r
	}

	panes, err := a.core.Tmux.ListPanes(serverID)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	var pane *tmuxx.Pane
	for i := range panes {
		if panes[i].SessionName != session {
			continue
		}
		if pane == nil || panes[i].WindowIndex < pane.WindowIndex {
			pane = &panes[i]
		}
	}
	if pane == nil {
		r.Error = fmt.Sprintf("tmux session %q is not running on this server", session)
		return r
	}
	r.Target = pane.PaneID

	if err := a.core.Tmux.SendText(serverID, pane.PaneID, message, execute); err != nil {
		r.Error = err.Error()
		return r
	}
	r.OK = true
	return r
}

// BroadcastTo delivers the same message to many recipients at once and returns
// one receipt each, so a fan-out to twenty is verifiable rather than hopeful.
// Work is issued concurrently but capped so a broadcast cannot open an
// unbounded number of SSH channels.
func (a *AgentService) BroadcastTo(targets []BroadcastTarget, message string, execute bool) []Receipt {
	out := make([]Receipt, len(targets))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup

	for i, t := range targets {
		wg.Add(1)
		go func(i int, t BroadcastTarget) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if t.AgentID != "" {
				out[i] = a.Send(t.AgentID, message, execute)
				return
			}
			out[i] = a.SendToSession(t.ServerID, t.Session, message, execute)
		}(i, t)
	}
	wg.Wait()
	return out
}

// Broadcast is the agent-only form, kept for callers that already hold ids.
func (a *AgentService) Broadcast(agentIDs []string, message string, execute bool) []Receipt {
	targets := make([]BroadcastTarget, len(agentIDs))
	for i, id := range agentIDs {
		targets[i] = BroadcastTarget{AgentID: id}
	}
	return a.BroadcastTo(targets, message, execute)
}

// QuickLaunch is the result of starting an agent straight from a directory.
type QuickLaunch struct {
	ServerID string `json:"serverId"`
	Dir      string `json:"dir"`
	Session  string `json:"session"`
	Command  string `json:"command"`
	Created  bool   `json:"createdSession"`
	Reused   bool   `json:"reusedSession"`
	AgentID  string `json:"agentId"`
}

// LaunchInDir starts an agent in a directory without any prior setup: it names
// a tmux session after the folder, creates it there, and types the command in.
//
// This is the path from "I am looking at a project directory" to "an agent is
// working in it" — the reason the file browser exists next to the tree rather
// than instead of it. Re-running it on a directory whose session already has
// something in the pane attaches instead of stacking a second agent on top.
func (a *AgentService) LaunchInDir(serverID, dir, command string) (QuickLaunch, error) {
	dir = strings.TrimRight(strings.TrimSpace(dir), "/")
	if serverID == "" || dir == "" {
		return QuickLaunch{}, errors.New("a server and a directory are required")
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return QuickLaunch{}, errors.New("pick a command to run")
	}

	if info := a.core.Tmux.Available(serverID); !info.Available {
		return QuickLaunch{}, fmt.Errorf("tmux is not available on this server: %s", info.Error)
	}

	base := path.Base(dir)
	if base == "" || base == "." || base == "/" {
		base = "root"
	}
	session := "agentmux/" + Slug(base)

	res := QuickLaunch{ServerID: serverID, Dir: dir, Session: session, Command: command}

	exists, err := a.core.Tmux.HasSession(serverID, session)
	if err != nil {
		return res, err
	}
	if !exists {
		if err := a.core.Tmux.NewSession(serverID, session, dir); err != nil {
			return res, err
		}
		res.Created = true
	}

	panes, err := a.core.Tmux.ListPanes(serverID)
	if err != nil {
		return res, err
	}
	var pane *tmuxx.Pane
	for i := range panes {
		if panes[i].SessionName == session {
			if pane == nil || panes[i].WindowIndex < pane.WindowIndex {
				pane = &panes[i]
			}
		}
	}
	if pane == nil {
		return res, fmt.Errorf("tmux session %q has no panes", session)
	}

	// Something is already running in there — most likely the agent from the
	// last time this directory was launched. Leave it be and let the caller
	// attach to it.
	if !shellCommands[pane.Command] {
		res.Reused = true
		return res, nil
	}

	full := "cd " + sshx.ShellQuote(dir) + " && " + command
	if a.core.IsWinHost(serverID) {
		full = "Set-Location " + psQuote(dir) + "; " + command
	}
	if err := a.core.Tmux.SendText(serverID, pane.PaneID, full, true); err != nil {
		return res, err
	}
	return res, nil
}

// Logs returns the tail of an agent's pane without attaching a terminal.
func (a *AgentService) Logs(agentID string, lines int) (string, error) {
	ag, ws, err := a.resolve(agentID)
	if err != nil {
		return "", err
	}
	target, err := a.targetFor(ag, ws.ServerID)
	if err != nil {
		return "", err
	}
	return a.core.Tmux.CapturePane(ws.ServerID, target, lines)
}

// Refresh polls one server and updates the status of every agent on it.
func (a *AgentService) Refresh(serverID string) ([]store.Agent, error) {
	if err := a.pollServer(serverID); err != nil {
		return nil, err
	}
	agents, err := a.core.Store.ListAgents()
	if err != nil {
		return nil, err
	}
	a.core.Emit("agents:updated", agents)
	return agents, nil
}

// RefreshAll polls only servers that already hold a live connection. Servers
// that are idle stay idle: with a hundred configured hosts, polling all of them
// would mean a hundred dials every few seconds.
func (a *AgentService) RefreshAll() ([]store.Agent, error) {
	for _, serverID := range a.connectedServersWithAgents() {
		_ = a.pollServer(serverID)
	}
	agents, err := a.core.Store.ListAgents()
	if err != nil {
		return nil, err
	}
	a.core.Emit("agents:updated", agents)
	return agents, nil
}

func (a *AgentService) connectedServersWithAgents() []string {
	agents, err := a.core.Store.ListAgents()
	if err != nil {
		return nil
	}
	workspaces, err := a.core.Store.ListWorkspaces()
	if err != nil {
		return nil
	}
	wsByID := map[string]store.Workspace{}
	for _, w := range workspaces {
		wsByID[w.ID] = w
	}

	seen := map[string]bool{}
	out := []string{}
	for _, ag := range agents {
		ws, ok := wsByID[ag.WorkspaceID]
		if !ok || seen[ws.ServerID] {
			continue
		}
		seen[ws.ServerID] = true
		if a.core.IsReachable(ws.ServerID) {
			out = append(out, ws.ServerID)
		}
	}
	return out
}

// pollServer refreshes every agent on one server with a single pane listing,
// plus one capture per running agent for its progress line.
func (a *AgentService) pollServer(serverID string) error {
	agents, err := a.core.Store.ListAgents()
	if err != nil {
		return err
	}
	workspaces, err := a.core.Store.ListWorkspaces()
	if err != nil {
		return err
	}
	wsByID := map[string]store.Workspace{}
	for _, w := range workspaces {
		wsByID[w.ID] = w
	}

	mine := []store.Agent{}
	for _, ag := range agents {
		if ws, ok := wsByID[ag.WorkspaceID]; ok && ws.ServerID == serverID {
			mine = append(mine, ag)
		}
	}
	if len(mine) == 0 {
		return nil
	}

	panes, err := a.core.Tmux.ListPanes(serverID)
	if err != nil {
		for _, ag := range mine {
			_ = a.core.Store.UpdateAgentRuntime(ag.ID, store.StatusUnknown, store.ActivityNone, nil, "")
		}
		return err
	}

	for _, ag := range mine {
		pane, ok := pickPane(ag, panes)
		if !ok {
			_ = a.core.Store.UpdateAgentRuntime(ag.ID, store.StatusDetached, store.ActivityNone, nil, "")
			continue
		}
		status := store.StatusIdle
		activity := store.ActivityNone
		progress := ""
		if !shellCommands[pane.Command] {
			status = store.StatusRunning
			if out, err := a.core.Tmux.CapturePane(serverID, pane.PaneID, 40); err == nil {
				progress = lastMeaningfulLine(out)
				activity = classifyActivity(out)
			} else {
				// Could not read the pane this cycle; guessing "working" would
				// invent state, and "quiet" would raise a false done-mark.
				activity = a.prevActivity(ag.ID)
			}
		}
		pid := pane.PID
		_ = a.core.Store.UpdateAgentRuntime(ag.ID, status, activity, &pid, progress)
		a.updateAttention(ag, activity)
	}
	return nil
}

func (a *AgentService) prevActivity(agentID string) store.AgentActivity {
	a.actMu.Lock()
	defer a.actMu.Unlock()
	return a.lastActivity[agentID]
}

// updateAttention turns this poll's activity into the sticky mark a person
// scans for. Marks are raised on transitions and stay up until acknowledged;
// an agent that goes back to work takes its stale mark down itself, because a
// "come look" that points at superseded output is worse than none.
func (a *AgentService) updateAttention(ag store.Agent, activity store.AgentActivity) {
	a.actMu.Lock()
	prev, known := a.lastActivity[ag.ID]
	a.lastActivity[ag.ID] = activity
	a.actMu.Unlock()

	switch {
	case activity == store.ActivityInput:
		if prev != store.ActivityInput && ag.Attention != store.AttentionInput {
			a.setAttention(ag.ID, store.AttentionInput)
		}
	case activity == store.ActivityWorking:
		if ag.Attention != store.AttentionNone {
			a.setAttention(ag.ID, store.AttentionNone)
		}
	// Working a moment ago, quiet (or exited) now: the run ended and there is
	// output to review. `known` guards the first poll after a restart, where
	// "no record" must not read as "was working".
	case known && prev == store.ActivityWorking:
		if ag.Attention != store.AttentionDone {
			a.setAttention(ag.ID, store.AttentionDone)
		}
	}
}

func (a *AgentService) setAttention(agentID string, att store.AgentAttention) {
	_ = a.core.Store.SetAgentAttention(agentID, att)
}

// Acknowledge clears an agent's attention mark. The UI calls it when a person
// actually looks at the agent — opens its terminal or dismisses the flag — so
// the mark measures "unseen", not "unanswered".
func (a *AgentService) Acknowledge(agentID string) error {
	if err := a.core.Store.SetAgentAttention(agentID, store.AttentionNone); err != nil {
		return err
	}
	a.emitAgents()
	return nil
}

// targetFor resolves the tmux target for an agent, preferring the stable pane
// id recorded at launch.
func (a *AgentService) targetFor(ag store.Agent, serverID string) (string, error) {
	panes, err := a.core.Tmux.ListPanes(serverID)
	if err != nil {
		return "", err
	}
	pane, ok := pickPane(ag, panes)
	if !ok {
		return "", fmt.Errorf("tmux session %q is not running on this server", ag.TmuxSession)
	}
	return pane.PaneID, nil
}

// pickPane finds the pane an agent lives in: the recorded pane id when it still
// exists, otherwise the lowest-numbered pane of the session.
func pickPane(ag store.Agent, panes []tmuxx.Pane) (tmuxx.Pane, bool) {
	var candidates []tmuxx.Pane
	for _, p := range panes {
		if p.SessionName != ag.TmuxSession {
			continue
		}
		if ag.TmuxPaneID != "" && p.PaneID == ag.TmuxPaneID {
			return p, true
		}
		candidates = append(candidates, p)
	}
	if len(candidates) == 0 {
		return tmuxx.Pane{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		wi, _ := strconv.Atoi(candidates[i].WindowIndex)
		wj, _ := strconv.Atoi(candidates[j].WindowIndex)
		if wi != wj {
			return wi < wj
		}
		pi, _ := strconv.Atoi(candidates[i].PaneIndex)
		pj, _ := strconv.Atoi(candidates[j].PaneIndex)
		return pi < pj
	})
	return candidates[0], true
}

// lastMeaningfulLine picks the last non-blank line of a pane capture, which is
// a good enough progress signal for agents that print status as they work.
func lastMeaningfulLine(capture string) string {
	lines := strings.Split(strings.ReplaceAll(capture, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if len([]rune(l)) > 160 {
			l = string([]rune(l)[:160]) + "…"
		}
		return l
	}
	return ""
}

func (a *AgentService) resolve(agentID string) (store.Agent, store.Workspace, error) {
	ag, err := a.core.Store.GetAgent(agentID)
	if err != nil {
		return store.Agent{}, store.Workspace{}, err
	}
	ws, err := a.core.Store.GetWorkspace(ag.WorkspaceID)
	if err != nil {
		return store.Agent{}, store.Workspace{}, err
	}
	return ag, ws, nil
}

func (a *AgentService) emitAgents() {
	if agents, err := a.core.Store.ListAgents(); err == nil {
		a.core.Emit("agents:updated", agents)
	}
}
