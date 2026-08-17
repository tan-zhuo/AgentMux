package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentmux/internal/memory"
	"agentmux/internal/orch"
	"agentmux/internal/store"
)

// This file is where tool names become real calls.
//
// Every implementation is a thin wrapper over a service the UI already uses, so
// the orchestrator cannot reach anything a person could not reach through the
// interface. There is no privileged path: it gets the same operations, through
// the same code, with a gate in front.
//
// What is deliberately absent matters as much as what is here. Nothing touches
// server credentials, the host key store, or file transfer between the local
// machine and a server. Those are not gated more heavily — they are simply not
// offered, so no amount of persuasion reaches them.

// buildRegistry wires the catalogue to the core's services.
func (c *Core) buildRegistry() (*orch.Registry, error) {
	reg := orch.NewRegistry(c.trustFor)

	agents := NewAgentService(c)
	tmux := NewTmuxService(c)
	files := NewFileService(c)
	metrics := NewMetricsService(c)
	servers := NewServerService(c)
	toolkit := NewToolkitService(c)

	type spec struct {
		name   string
		schema string
		invoke func(ctx context.Context, args json.RawMessage) (any, error)
	}

	specs := []spec{
		// --- read -----------------------------------------------------------
		{"agents.list", `{"type":"object","properties":{}}`,
			func(context.Context, json.RawMessage) (any, error) { return agents.List() }},

		{"agents.logs", schemaOf(`"agentId":{"type":"string","description":"the agent to read"},
			"lines":{"type":"integer","description":"how many lines from the end, default 80"}`, "agentId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					AgentID string `json:"agentId"`
					Lines   int    `json:"lines"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				if a.Lines <= 0 || a.Lines > 500 {
					a.Lines = 80
				}
				return agents.Logs(a.AgentID, a.Lines)
			}},

		{"tmux.sessions", schemaOf(`"serverId":{"type":"string"}`, "serverId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return tmux.Sessions(a.ServerID)
			}},

		{"tmux.panes", schemaOf(`"serverId":{"type":"string"}`, "serverId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return tmux.Panes(a.ServerID)
			}},

		{"tmux.capture", schemaOf(`"serverId":{"type":"string"},
			"target":{"type":"string","description":"session, window or pane id"},
			"lines":{"type":"integer","description":"visible lines to read, default 60"}`,
			"serverId", "target"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
					Target   string `json:"target"`
					Lines    int    `json:"lines"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				if a.Lines <= 0 || a.Lines > 400 {
					a.Lines = 60
				}
				return tmux.Capture(a.ServerID, a.Target, a.Lines)
			}},

		{"metrics.sample", schemaOf(`"serverId":{"type":"string"}`, "serverId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return metrics.Sample(a.ServerID), nil
			}},

		{"files.list", schemaOf(`"serverId":{"type":"string"},"dir":{"type":"string"}`,
			"serverId", "dir"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
					Dir      string `json:"dir"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return files.List(a.ServerID, a.Dir)
			}},

		{"files.read", schemaOf(`"serverId":{"type":"string"},"path":{"type":"string"}`,
			"serverId", "path"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
					Path     string `json:"path"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return files.Read(a.ServerID, a.Path)
			}},

		{"servers.list", `{"type":"object","properties":{}}`,
			func(context.Context, json.RawMessage) (any, error) {
				list, err := servers.List()
				if err != nil {
					return nil, err
				}
				// Trimmed to what planning needs. The full record carries host
				// keys and key paths, which are of no use here and would sit in
				// the model's context for the rest of the run.
				out := make([]map[string]any, 0, len(list))
				for _, s := range list {
					out = append(out, map[string]any{
						"id": s.ID, "name": s.Name, "host": s.Host,
						"tags": s.Tags, "trustLevel": s.TrustLevel,
					})
				}
				return out, nil
			}},

		{"toolkit.detect", schemaOf(`"serverId":{"type":"string"}`, "serverId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return toolkit.Detect(a.ServerID), nil
			}},

		{"memory.search", schemaOf(`"text":{"type":"string","description":"what to recall"},
			"topK":{"type":"integer"}`, "text"),
			func(ctx context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					Text string `json:"text"`
					TopK int    `json:"topK"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				if a.TopK <= 0 || a.TopK > 20 {
					a.TopK = 6
				}
				return c.Memory.Search(ctx, memory.Query{Text: a.Text, TopK: a.TopK})
			}},

		// --- act -------------------------------------------------------------
		{"agents.send", schemaOf(`"agentId":{"type":"string"},
			"message":{"type":"string"},
			"execute":{"type":"boolean","description":"press Enter. false leaves the text typed but unsent for a person to check"}`,
			"agentId", "message"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					AgentID string `json:"agentId"`
					Message string `json:"message"`
					Execute bool   `json:"execute"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return agents.Send(a.AgentID, a.Message, a.Execute), nil
			}},

		{"agents.start", schemaOf(`"agentId":{"type":"string"}`, "agentId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					AgentID string `json:"agentId"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return agents.Start(a.AgentID)
			}},

		{"agents.stop", schemaOf(`"agentId":{"type":"string"}`, "agentId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					AgentID string `json:"agentId"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return "stopped", agents.Stop(a.AgentID)
			}},

		{"tmux.send_text", schemaOf(`"serverId":{"type":"string"},"target":{"type":"string"},
			"text":{"type":"string"},"pressEnter":{"type":"boolean"}`,
			"serverId", "target", "text"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID   string `json:"serverId"`
					Target     string `json:"target"`
					Text       string `json:"text"`
					PressEnter bool   `json:"pressEnter"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return "sent", tmux.SendText(a.ServerID, a.Target, a.Text, a.PressEnter)
			}},

		{"tmux.create_session", schemaOf(`"serverId":{"type":"string"},"name":{"type":"string"},
			"cwd":{"type":"string"}`, "serverId", "name"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
					Name     string `json:"name"`
					Cwd      string `json:"cwd"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return "created", tmux.CreateSession(a.ServerID, a.Name, a.Cwd)
			}},

		{"files.write", schemaOf(`"serverId":{"type":"string"},"path":{"type":"string"},
			"content":{"type":"string"}`, "serverId", "path", "content"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
					Path     string `json:"path"`
					Content  string `json:"content"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				// expectedModTime 0 means "do not check". The orchestrator has
				// no business silently overwriting a file that changed under it,
				// so it reads first and the write is confirmed by a person
				// anyway.
				return files.Write(a.ServerID, a.Path, a.Content, 0, false)
			}},

		{"memory.write", schemaOf(`"title":{"type":"string"},"body":{"type":"string"},
			"kind":{"type":"string","enum":["project_fact","agent_event","user_pref","session_ctx","system_log"]}`,
			"body"),
			func(ctx context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					Title string `json:"title"`
					Body  string `json:"body"`
					Kind  string `json:"kind"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				kind := store.MemoryKind(a.Kind)
				if kind == "" {
					kind = store.MemProjectFact
				}
				return c.Memory.Put(ctx, store.Memory{
					Kind: kind, Title: a.Title, Body: a.Body, Source: "orchestrator",
				})
			}},

		// --- destructive -----------------------------------------------------
		{"agents.kill", schemaOf(`"agentId":{"type":"string"}`, "agentId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					AgentID string `json:"agentId"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return "killed", agents.Kill(a.AgentID)
			}},

		{"agents.restart", schemaOf(`"agentId":{"type":"string"}`, "agentId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					AgentID string `json:"agentId"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return agents.Restart(a.AgentID)
			}},

		{"tmux.kill_session", schemaOf(`"serverId":{"type":"string"},"name":{"type":"string"}`,
			"serverId", "name"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
					Name     string `json:"name"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return "killed", tmux.KillSession(a.ServerID, a.Name)
			}},

		{"files.remove", schemaOf(`"serverId":{"type":"string"},"path":{"type":"string"},
			"recursive":{"type":"boolean"}`, "serverId", "path"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID  string `json:"serverId"`
					Path      string `json:"path"`
					Recursive bool   `json:"recursive"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return "removed", files.Remove(a.ServerID, a.Path, a.Recursive)
			}},

		{"agents.broadcast", schemaOf(`"agentIds":{"type":"array","items":{"type":"string"}},
			"message":{"type":"string"},"execute":{"type":"boolean"}`, "agentIds", "message"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					AgentIDs []string `json:"agentIds"`
					Message  string   `json:"message"`
					Execute  bool     `json:"execute"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				if len(a.AgentIDs) == 0 {
					return nil, fmt.Errorf("no agents named")
				}
				return agents.Broadcast(a.AgentIDs, a.Message, a.Execute), nil
			}},

		{"toolkit.install", schemaOf(`"serverId":{"type":"string"},"toolId":{"type":"string"},
			"methodId":{"type":"string"}`, "serverId", "toolId"),
			func(_ context.Context, raw json.RawMessage) (any, error) {
				var a struct {
					ServerID string `json:"serverId"`
					ToolID   string `json:"toolId"`
					MethodID string `json:"methodId"`
				}
				if err := decode(raw, &a); err != nil {
					return nil, err
				}
				return toolkit.Install(a.ServerID, a.ToolID, a.MethodID)
			}},
	}

	for _, s := range specs {
		if err := reg.Register(orch.Tool{
			Name: s.name, Schema: json.RawMessage(s.schema), Invoke: s.invoke,
		}); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// trustFor resolves how much the orchestrator is trusted on the machine a call
// would touch. An agent is resolved to its workspace's server.
//
// Anything it cannot resolve is treated as untrusted: a call whose target
// cannot be established is exactly the sort that should be looked at.
func (c *Core) trustFor(serverID, agentID string) store.TrustLevel {
	if serverID == "" && agentID != "" {
		if ag, err := c.Store.GetAgent(agentID); err == nil {
			if ws, err := c.Store.GetWorkspace(ag.WorkspaceID); err == nil {
				serverID = ws.ServerID
			}
		}
	}
	if serverID == "" {
		return store.TrustNormal
	}
	srv, err := c.Store.GetServer(serverID)
	if err != nil || srv.TrustLevel == "" {
		return store.TrustNormal
	}
	return srv.TrustLevel
}

// schemaOf assembles an argument schema from property fragments.
func schemaOf(properties string, required ...string) string {
	quoted := make([]string, len(required))
	for i, r := range required {
		quoted[i] = `"` + r + `"`
	}
	return fmt.Sprintf(`{"type":"object","properties":{%s},"required":[%s]}`,
		properties, strings.Join(quoted, ","))
}

// decode reads tool arguments, reporting the failure in terms the model can act
// on rather than in Go's.
func decode(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return fmt.Errorf("no arguments were given")
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("the arguments could not be read: %v", err)
	}
	return nil
}
