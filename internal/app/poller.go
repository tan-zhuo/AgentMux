package app

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"agentmux/internal/store"
)

// StartPoller refreshes agent status in the background. It only touches servers
// that already have a live SSH connection, so the cost is proportional to what
// the user is actually watching rather than to how many servers are configured.
func (c *Core) StartPoller(svc *AgentService, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		var lastSignature string
		for {
			select {
			case <-c.stopCh:
				return
			case <-ticker.C:
				// Servers are polled together rather than one after another.
				// A host that stops answering at the TCP level holds its
				// command open until the keepalive gives up on it, which is
				// most of a minute — and in a queue that is most of a minute
				// in which every other host's agents are frozen too, for a
				// problem none of them have. The cap is the same one the
				// metrics panel uses for the same reason: enough to overlap
				// the waiting, not so many that a fleet becomes a stampede.
				ids := svc.connectedServersWithAgents()
				sem := make(chan struct{}, 6)
				var wg sync.WaitGroup
				for _, serverID := range ids {
					select {
					case <-c.stopCh:
						return
					default:
					}
					wg.Add(1)
					go func(id string) {
						defer wg.Done()
						sem <- struct{}{}
						defer func() { <-sem }()
						_ = svc.pollServer(id)
					}(serverID)
				}
				wg.Wait()

				agents, err := c.Store.ListAgents()
				if err != nil {
					continue
				}
				// Only publish when something a user could see has changed.
				// Emitting on every tick replaced the agent list in the UI store
				// twice a minute per agent, re-rendering panels that had nothing
				// new to show.
				sig := agentsSignature(agents)
				if sig == lastSignature {
					continue
				}
				lastSignature = sig
				c.Emit("agents:updated", agents)
			}
		}
	}()
}

// agentsSignature captures the fields the UI renders. LastSeen is deliberately
// excluded: the poller rewrites it every cycle by definition, so including it
// would make every tick look like a change and defeat the whole comparison.
//
// The definition fields are in here as well as the runtime ones. An edit
// publishes itself, so this is the backstop for a change that arrives some
// other way — a second window, a restored database — and leaving it out meant
// a rename could sit unnoticed for as long as the agent's status held still.
func agentsSignature(agents []store.Agent) string {
	var b strings.Builder
	for _, a := range agents {
		b.WriteString(a.ID)
		b.WriteByte('\x1f')
		b.WriteString(a.Name)
		b.WriteByte('\x1f')
		b.WriteString(a.Command)
		b.WriteByte('\x1f')
		b.WriteString(a.TmuxSession)
		b.WriteByte('\x1f')
		b.WriteString(a.WorkspaceID)
		b.WriteByte('\x1f')
		b.WriteString(string(a.Status))
		b.WriteByte('\x1f')
		b.WriteString(string(a.Activity))
		b.WriteByte('\x1f')
		b.WriteString(string(a.Attention))
		b.WriteByte('\x1f')
		if a.PID != nil {
			b.WriteString(strconv.Itoa(*a.PID))
		}
		b.WriteByte('\x1f')
		b.WriteString(a.ProgressText)
		b.WriteByte('\x1e')
	}
	return b.String()
}
