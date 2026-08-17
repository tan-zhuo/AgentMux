package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"agentmux/internal/store"
)

// fleetObserver renders the current state of the fleet as text.
//
// It reads the local database rather than going out to every server: the poller
// already keeps agent status current for anything connected, and a run that
// began by opening SSH connections to a hundred machines would be a denial of
// service against its own user. Anything the model wants in more detail, it can
// ask for with a read tool.
type fleetObserver struct{ core *Core }

// Observe returns a plain-text picture of servers, agents and their states.
func (o fleetObserver) Observe(_ context.Context, projectID string) (string, error) {
	servers, err := o.core.Store.ListServers()
	if err != nil {
		return "", err
	}
	agents, err := o.core.Store.ListAgents()
	if err != nil {
		return "", err
	}
	workspaces, err := o.core.Store.ListWorkspaces()
	if err != nil {
		return "", err
	}

	wsByID := map[string]store.Workspace{}
	for _, w := range workspaces {
		wsByID[w.ID] = w
	}
	connected := map[string]bool{}
	for _, s := range servers {
		connected[s.ID] = o.core.IsReachable(s.ID)
	}

	var b strings.Builder
	b.WriteString("Servers:\n")
	for _, s := range servers {
		state := "not connected"
		if connected[s.ID] {
			state = "connected"
		}
		fmt.Fprintf(&b, "- %s (id %s, host %s, trust %s) — %s\n",
			s.Name, s.ID, s.Host, s.TrustLevel, state)
	}
	if len(servers) == 0 {
		b.WriteString("- none configured\n")
	}

	b.WriteString("\nAgents:\n")
	shown := 0
	for _, a := range agents {
		ws, ok := wsByID[a.WorkspaceID]
		if projectID != "" && (!ok || ws.ProjectID != projectID) {
			continue
		}
		shown++
		fmt.Fprintf(&b, "- %s (id %s) status %s, session %s", a.Name, a.ID, a.Status, a.TmuxSession)
		if ok {
			fmt.Fprintf(&b, ", server %s, path %s", ws.ServerID, ws.RemotePath)
		}
		if a.LastSeen != nil {
			since := time.Since(time.Unix(*a.LastSeen, 0)).Round(time.Second)
			fmt.Fprintf(&b, ", last seen %s ago", since)
		}
		if a.ProgressText != "" {
			fmt.Fprintf(&b, "\n  last line: %s", a.ProgressText)
		}
		b.WriteString("\n")
	}
	if shown == 0 {
		b.WriteString("- none\n")
	}
	return b.String(), nil
}
