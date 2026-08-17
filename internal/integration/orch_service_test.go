package integration

import (
	"encoding/json"
	"strings"
	"testing"

	"agentmux/internal/app"
	"agentmux/internal/orch"
	"agentmux/internal/orch/catalog"
	"agentmux/internal/store"
)

// TestOrchestratorIsOffUntilAskedFor covers the default posture: installing
// AgentMux must not produce something that acts.
func TestOrchestratorIsOffUntilAskedFor(t *testing.T) {
	core := newCore(t)
	svc := app.NewOrchService(core)

	cfg := svc.Config()
	if cfg.Enabled {
		t.Fatal("the orchestrator should be off on a fresh profile")
	}
	if cfg.PatrolMinutes != 0 {
		t.Errorf("patrols should be off by default, got every %d minutes", cfg.PatrolMinutes)
	}

	if _, err := svc.Start("do something", ""); err == nil {
		t.Fatal("starting a run while switched off should be refused")
	} else if !strings.Contains(err.Error(), "switched off") {
		t.Errorf("the refusal should say why, got %q", err)
	}

	if _, err := svc.SaveConfig(app.OrchConfig{Enabled: true, PatrolMinutes: 1}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	// A one-minute patrol is a battery drain against a fleet that changes far
	// more slowly, so it is raised rather than honoured.
	if got := svc.Config().PatrolMinutes; got != 5 {
		t.Errorf("a too-frequent patrol should be clamped to 5, got %d", got)
	}
}

// TestEveryCatalogueToolIsImplemented guards against a silent capability gap: a
// tool the model is told about but that nothing implements is refused at the
// gate, which reads to a user as the orchestrator being mysteriously useless.
func TestEveryCatalogueToolIsImplemented(t *testing.T) {
	core := newCore(t)

	// Both a human run and a patrol, since the patrol sees a smaller set.
	human := map[string]bool{}
	for _, tool := range core.Orch.Tools(orch.TriggerHuman) {
		human[tool.Name] = true
	}
	for _, meta := range catalog.All() {
		if !human[meta.Name] {
			t.Errorf("%s is in the catalogue but nothing implements it", meta.Name)
		}
	}

	for _, tool := range core.Orch.Tools(orch.TriggerSchedule) {
		if tool.Risk != catalog.RiskRead {
			t.Errorf("a patrol is offered %s, which is %s", tool.Name, tool.Risk)
		}
	}
}

// TestTrustIsResolvedThroughTheAgent checks the path that decides whether a
// call is confirmed: most tools name an agent, not a server.
func TestTrustIsResolvedThroughTheAgent(t *testing.T) {
	core := newCore(t)

	srv, err := core.Store.SaveServer(store.ServerInput{
		Name: "trusted-box", Host: "127.0.0.1", Username: "u", TrustLevel: store.TrustTrusted,
	})
	if err != nil {
		t.Fatalf("save server: %v", err)
	}
	project, err := core.Store.SaveProject(store.Project{Name: "p"})
	if err != nil {
		t.Fatalf("save project: %v", err)
	}
	ws, err := core.Store.SaveWorkspace(store.Workspace{
		ProjectID: project.ID, ServerID: srv.ID, Name: "w", RemotePath: "/tmp",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	agent, err := core.Store.SaveAgent(store.Agent{
		WorkspaceID: ws.ID, Name: "a", Command: "claude", TmuxSession: "s",
	})
	if err != nil {
		t.Fatalf("save agent: %v", err)
	}

	args, _ := json.Marshal(map[string]string{"agentId": agent.ID})

	// An agent on a trusted server: a recoverable action runs unattended.
	d := core.Orch.Explain("agents.send", orch.TriggerHuman, args)
	if d.Verdict != orch.Allow {
		t.Errorf("agents.send on a trusted server should run: %s (%s)", d.Verdict, d.Reason)
	}
	// The same agent, a destructive action: still confirmed. Trust buys
	// recoverable actions, never irreversible ones.
	d = core.Orch.Explain("agents.kill", orch.TriggerHuman, args)
	if d.Verdict != orch.Ask {
		t.Errorf("agents.kill should be confirmed even on a trusted server: %s", d.Verdict)
	}

	// An agent whose server says nothing gets the cautious default.
	unknown, _ := json.Marshal(map[string]string{"agentId": "does-not-exist"})
	d = core.Orch.Explain("agents.send", orch.TriggerHuman, unknown)
	if d.Verdict != orch.Ask {
		t.Errorf("an unresolvable target should be treated as untrusted, got %s", d.Verdict)
	}
}
