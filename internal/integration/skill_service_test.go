package integration

import (
	"strings"
	"testing"

	"agentmux/internal/app"
	"agentmux/internal/skill"
	"agentmux/internal/store"
)

// TestSkillLifecycleThroughTheService walks the path the panel walks, against
// the real core, with no model runtime anywhere. Everything except matching has
// to work on a machine where Ollama was never installed.
func TestSkillLifecycleThroughTheService(t *testing.T) {
	core := newCore(t)
	svc := app.NewSkillService(core)

	created, err := svc.Create(store.Skill{
		Name:        "Stuck agent recovery",
		Description: "An agent that has gone quiet",
		Trigger:     "an agent has been idle or unresponsive for more than half an hour",
		Steps: []store.SkillStep{
			{Order: 1, Description: "confirm the pane is idle", RecommendedTools: []string{"tmux.capture"}},
			{Order: 2, Description: "ask it what it is doing", RecommendedTools: []string{"agents.send"}},
		},
		Constraints: []string{"never kill a session that is mid-write"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Status != store.SkillActive || created.Version != 1 {
		t.Fatalf("a hand-written skill should start active at v1: %+v", created)
	}
	if created.HasVector {
		t.Error("nothing could have embedded it with no runtime running")
	}

	stats, err := svc.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Active != 1 || stats.Pending != 1 {
		t.Errorf("the library should report one active, one pending: %+v", stats)
	}

	// Matching needs the embedder, so it must fail rather than quietly
	// returning nothing — an empty list would read as "no skill applies".
	if _, err := svc.Match(skill.Query{Text: "an agent is stuck", TopK: 5}); err == nil {
		t.Error("matching should fail when the embedder is unreachable")
	}

	// Edit, then roll back.
	edited := created
	edited.Trigger = "an agent has been idle for more than ten minutes"
	updated, err := svc.Update(edited, "tightened the window")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("an edit should make v2, got v%d", updated.Version)
	}

	versions, err := svc.Versions(created.ID)
	if err != nil || len(versions) != 2 {
		t.Fatalf("history: %d versions, %v", len(versions), err)
	}

	rolled, err := svc.Rollback(created.ID, 1)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if !strings.Contains(rolled.Trigger, "half an hour") {
		t.Errorf("the rollback did not restore the original trigger: %q", rolled.Trigger)
	}

	// Lifecycle.
	if _, err := svc.Apply(created.ID, "disable"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := svc.Apply(created.ID, "approve"); err == nil {
		t.Error("approving a disabled skill is not a move that exists")
	}
	if _, err := svc.Apply(created.ID, "enable"); err != nil {
		t.Fatalf("enable: %v", err)
	}

	// Export and import, which is how a skill reaches another machine.
	bundle, err := svc.ExportJSON(nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(bundle, "Stuck agent recovery") {
		t.Errorf("the export does not contain the skill: %s", bundle)
	}
	md, err := svc.ExportMarkdown(nil)
	if err != nil {
		t.Fatalf("export markdown: %v", err)
	}
	if !strings.Contains(md, "never kill a session that is mid-write") {
		t.Error("the Markdown export dropped the constraints")
	}

	if err := svc.Delete(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if rows, _ := svc.List(store.SkillFilter{}); len(rows) != 0 {
		t.Errorf("the library should be empty, got %d", len(rows))
	}
}

// TestSkillToolsAreTheRealCatalogue guards the editor's tool picker against
// offering something the validator will then refuse.
func TestSkillToolsAreTheRealCatalogue(t *testing.T) {
	core := newCore(t)
	svc := app.NewSkillService(core)

	tools := svc.Tools()
	if len(tools) < 10 {
		t.Fatalf("the catalogue looks empty: %d tools", len(tools))
	}

	var destructive int
	for _, tool := range tools {
		if tool.Description == "" {
			t.Errorf("%s has no description; the model reads these", tool.Name)
		}
		switch tool.Risk {
		case "read", "act", "destructive":
		default:
			t.Errorf("%s has risk %q", tool.Name, tool.Risk)
		}
		if tool.Risk == "destructive" {
			destructive++
		}
		// Every offered tool must be acceptable to the validator, or the
		// picker would hand people a name that cannot be saved.
		_, err := svc.Create(store.Skill{
			Name:    "probe " + tool.Name,
			Trigger: "a situation that calls for " + tool.Name,
			Steps:   []store.SkillStep{{Order: 1, Description: "use it", RecommendedTools: []string{tool.Name}}},
		})
		if err != nil {
			t.Errorf("the editor offers %s but saving it fails: %v", tool.Name, err)
		}
	}
	if destructive == 0 {
		t.Error("no tool is marked destructive, which cannot be right")
	}
}
