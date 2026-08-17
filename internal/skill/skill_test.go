package skill_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"agentmux/internal/skill"
	"agentmux/internal/store"
)

// bagOfWords embeds text as a presence vector over a fixed vocabulary, so two
// texts about the same thing score high without a model being involved.
type bagOfWords struct{ fail error }

var vocab = []string{
	"payment", "latency", "slow", "database", "index", "deploy", "release",
	"restart", "stuck", "idle", "agent", "disk", "full", "log", "rotate",
	"test", "flaky", "retry", "memory", "leak",
}

func (b *bagOfWords) Embed(_ context.Context, _ string, texts []string) ([][]float32, error) {
	if b.fail != nil {
		return nil, b.fail
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, len(vocab)+1)
		lower := strings.ToLower(t)
		for j, w := range vocab {
			if strings.Contains(lower, w) {
				v[j] = 1
			}
		}
		v[len(vocab)] = 0.01 // keeps a vocabulary-free text from being a zero vector
		out[i] = v
	}
	return out, nil
}

func newManager(t *testing.T, emb skill.Embedder) (*skill.Manager, *store.Store) {
	t.Helper()
	t.Setenv("AGENTMUX_DATA_DIR", t.TempDir())

	st, err := store.Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return skill.NewManager(st, emb, func() string { return "fake-v1" }), st
}

func sample(name, trigger string, steps ...string) store.Skill {
	sk := store.Skill{Name: name, Trigger: trigger, Description: name}
	for i, s := range steps {
		sk.Steps = append(sk.Steps, store.SkillStep{Order: i + 1, Description: s})
	}
	return sk
}

func create(t *testing.T, m *skill.Manager, sk store.Skill) store.Skill {
	t.Helper()
	out, err := m.Create(context.Background(), sk)
	if err != nil {
		t.Fatalf("create %q: %v", sk.Name, err)
	}
	return out
}

func TestMatchFindsTheRightSkillAndExplainsItself(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})

	create(t, m, sample("Payment latency triage",
		"the payment service is slow, latency is up under load",
		"read the metrics", "check the database index"))
	create(t, m, sample("Release deploy",
		"a deploy is going out from the release branch",
		"run the tests", "deploy"))
	create(t, m, sample("Stuck agent",
		"an agent has been idle or stuck for a long time",
		"read its log", "restart it"))
	create(t, m, sample("Disk filling up",
		"the disk is full or filling on a server",
		"find the big files", "rotate the log"))
	create(t, m, sample("Flaky test hunt",
		"a test is flaky and needs a retry loop to reproduce",
		"run it in a loop", "collect the failures"))

	matches, err := m.Match(context.Background(), skill.Query{
		Text: "the payment endpoint got slow again, latency spiked", TopK: 3,
	})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("nothing matched")
	}
	if matches[0].Skill.Name != "Payment latency triage" {
		t.Errorf("wrong skill first: %q", matches[0].Skill.Name)
	}
	// A bare score is not something a person can argue with.
	if !strings.Contains(matches[0].Reason, "similar") {
		t.Errorf("the match should explain itself, got %q", matches[0].Reason)
	}
	for i := 1; i < len(matches); i++ {
		if matches[i].Score > matches[i-1].Score {
			t.Error("matches are not sorted by score")
		}
	}
}

// TestDraftsAreNeverMatched is the guard on the whole review model: an
// unapproved skill must not be able to shape a plan.
func TestDraftsAreNeverMatched(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})

	draft := create(t, m, func() store.Skill {
		sk := sample("Draft one", "the disk is full on a server", "delete things")
		sk.CreatedBy = "orchestrator"
		return sk
	}())
	if draft.Status != store.SkillDraft {
		t.Fatalf("an orchestrator-authored skill should start as a draft, got %s", draft.Status)
	}
	if draft.HasVector {
		t.Error("a draft must not be embedded at all")
	}

	matches, err := m.Match(context.Background(), skill.Query{Text: "the disk is full", TopK: 5})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("a draft was matched: %+v", matches)
	}

	approved, err := m.Apply(context.Background(), draft.ID, skill.Approve)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != store.SkillActive || !approved.HasVector {
		t.Fatalf("approving should activate and embed: %+v", approved)
	}

	matches, err = m.Match(context.Background(), skill.Query{Text: "the disk is full", TopK: 5})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("after approval the skill should match, got %d", len(matches))
	}
}

func TestDisablingRemovesASkillFromMatchingImmediately(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})
	sk := create(t, m, sample("Stuck agent", "an agent is stuck or idle", "restart it"))

	if _, err := m.Apply(context.Background(), sk.ID, skill.Disable); err != nil {
		t.Fatalf("disable: %v", err)
	}
	matches, err := m.Match(context.Background(), skill.Query{Text: "an agent is stuck", TopK: 5})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(matches) != 0 {
		t.Error("a disabled skill was still matched")
	}

	if _, err := m.Apply(context.Background(), sk.ID, skill.Enable); err != nil {
		t.Fatalf("enable: %v", err)
	}
	matches, err = m.Match(context.Background(), skill.Query{Text: "an agent is stuck", TopK: 5})
	if err != nil {
		t.Fatalf("match: %v", err)
	}
	if len(matches) != 1 {
		t.Error("re-enabling should put it back in the pool")
	}
}

func TestImpossibleTransitionsAreRefused(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})
	sk := create(t, m, sample("Active one", "something happens on a server", "do a thing"))

	// Approving something already active is not a no-op, it is a move that
	// does not exist — and saying so beats silently doing nothing.
	if _, err := m.Apply(context.Background(), sk.ID, skill.Approve); err == nil {
		t.Error("approving an active skill should be refused")
	}
	if _, err := m.Apply(context.Background(), sk.ID, skill.Restore); err == nil {
		t.Error("restoring a skill that was never archived should be refused")
	}
	if _, err := m.Apply(context.Background(), sk.ID, skill.Enable); err == nil {
		t.Error("enabling an already active skill should be refused")
	}
}

func TestScopeIsAHardFilter(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})

	scoped := sample("Project deploy", "a deploy is going out from the release branch", "deploy")
	scoped.Scope = store.SkillProject
	scoped.ProjectIDs = []string{"project-a"}
	create(t, m, scoped)

	q := skill.Query{Text: "we are doing a release deploy", TopK: 5}
	if got, err := m.Match(context.Background(), q); err != nil || len(got) != 0 {
		t.Fatalf("a project skill must not match with no project in the query: %d %v", len(got), err)
	}

	q.ProjectID = "project-b"
	if got, _ := m.Match(context.Background(), q); len(got) != 0 {
		t.Error("a project skill must not match another project")
	}

	q.ProjectID = "project-a"
	if got, _ := m.Match(context.Background(), q); len(got) != 1 {
		t.Error("a project skill should match its own project")
	}
}

func TestEditKeepsHistoryAndRollbackRestoresIt(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})
	sk := create(t, m, sample("Payment triage", "the payment service is slow", "read the metrics"))

	edited := sk
	edited.Steps = []store.SkillStep{
		{Order: 1, Description: "read the metrics"},
		{Order: 2, Description: "check the database index"},
	}
	edited.Trigger = "the payment service is slow under load"
	updated, err := m.Update(context.Background(), edited, "added the index step")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Version != 2 || len(updated.Steps) != 2 {
		t.Fatalf("after the edit: v%d, %d steps", updated.Version, len(updated.Steps))
	}

	versions, err := m.Versions(sk.ID)
	if err != nil {
		t.Fatalf("versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected the creation and the pre-edit snapshot, got %d", len(versions))
	}

	rolled, err := m.Rollback(context.Background(), sk.ID, 1)
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if len(rolled.Steps) != 1 || rolled.Steps[0].Description != "read the metrics" {
		t.Fatalf("rollback did not restore the old content: %+v", rolled.Steps)
	}
	// History is append-only: the rollback is a new version, not an erasure.
	if rolled.Version != 3 {
		t.Errorf("a rollback should add a version, got v%d", rolled.Version)
	}
	if versions, _ := m.Versions(sk.ID); len(versions) != 3 {
		t.Errorf("the history should still hold every version, got %d", len(versions))
	}
	// And the restored wording has to be what is matched from now on.
	if !rolled.HasVector {
		t.Error("a rolled-back active skill should be re-embedded")
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})

	original := sample("Payment triage", "the payment service is slow under load",
		"read the metrics", "check the database index")
	original.Constraints = []string{"never restart during business hours"}
	original.Steps[0].RecommendedTools = []string{"metrics.sample", "agents.logs"}
	original.Examples = store.SkillExamples{Success: "found a missing index"}
	create(t, m, original)

	bundle, err := m.ExportJSON(nil)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// Import into a completely separate library, which is the case that
	// actually matters: moving a skill to another machine.
	other, _ := newManager(t, &bagOfWords{})
	res, err := other.ImportJSON(context.Background(), bundle)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 || len(res.Skipped) != 0 {
		t.Fatalf("import result: %+v", res)
	}

	got, err := other.List(store.SkillFilter{})
	if err != nil || len(got) != 1 {
		t.Fatalf("list after import: %d %v", len(got), err)
	}
	imported := got[0]

	if imported.Name != original.Name || imported.Trigger != original.Trigger {
		t.Errorf("identity changed: %q / %q", imported.Name, imported.Trigger)
	}
	if len(imported.Steps) != 2 || imported.Steps[1].Description != "check the database index" {
		t.Errorf("steps changed: %+v", imported.Steps)
	}
	if len(imported.Steps[0].RecommendedTools) != 2 {
		t.Errorf("recommended tools were lost: %+v", imported.Steps[0])
	}
	if len(imported.Constraints) != 1 || imported.Examples.Success != "found a missing index" {
		t.Errorf("constraints or examples were lost: %+v", imported)
	}
	// Local history must not travel: a freshly imported skill has not been used.
	if imported.UsageCount != 0 || imported.Version != 1 {
		t.Errorf("the imported copy claimed a history: used %d, v%d",
			imported.UsageCount, imported.Version)
	}
}

func TestImportSkipsBadSkillsWithoutLosingGoodOnes(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})

	bundle := `{"format":"agentmux.skills/v1","skills":[
	  {"name":"Good one","trigger":"the disk is full on a server","status":"active",
	   "steps":[{"order":1,"description":"find the big files"}]},
	  {"name":"Bad tool","trigger":"an agent is stuck and needs help","status":"active",
	   "steps":[{"order":1,"description":"do it","recommendedTools":["agents.selfDestruct"]}]},
	  {"name":"No steps","trigger":"something happens somewhere","status":"active","steps":[]}
	]}`

	res, err := m.ImportJSON(context.Background(), bundle)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 {
		t.Errorf("the good skill should have been imported, got %d", res.Imported)
	}
	if len(res.Skipped) != 2 {
		t.Fatalf("expected two refusals, got %v", res.Skipped)
	}
	// The refusal has to name what is wrong, or nobody can fix the file.
	if !strings.Contains(strings.Join(res.Skipped, " "), "agents.selfDestruct") {
		t.Errorf("the refusal should name the unknown tool: %v", res.Skipped)
	}
}

func TestValidationRejectsWhatCannotWork(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})

	cases := []struct {
		name string
		sk   store.Skill
		want string
	}{
		{"no name", sample("", "something happens on a server", "do it"), "name"},
		{"no steps", sample("Nameless", "something happens on a server"), "step"},
		{"short trigger", sample("Thing", "eh", "do it"), "trigger"},
		{"unknown tool", func() store.Skill {
			sk := sample("Thing", "something happens on a server", "do it")
			sk.Steps[0].RecommendedTools = []string{"rm.rf"}
			return sk
		}(), "not a tool"},
		{"project scope with no project", func() store.Skill {
			sk := sample("Thing", "something happens on a server", "do it")
			sk.Scope = store.SkillProject
			return sk
		}(), "project"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := m.Create(context.Background(), c.sk)
			if err == nil {
				t.Fatal("expected a refusal")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("the message should mention %q, got %q", c.want, err)
			}
		})
	}
}

func TestStepsAreRenumberedAndDeduplicated(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{})

	sk := store.Skill{
		Name: "Messy", Trigger: "a messy situation on a server",
		Steps: []store.SkillStep{
			{Order: 1, Description: "read the log"},
			{Order: 1, Description: "  read the log  "}, // same step, sloppier
			{Order: 1, Description: "restart it"},
			{Order: 9, Description: ""},
		},
	}
	saved, err := m.Create(context.Background(), sk)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(saved.Steps) != 2 {
		t.Fatalf("expected the duplicate and the empty step to go, got %+v", saved.Steps)
	}
	if saved.Steps[0].Order != 1 || saved.Steps[1].Order != 2 {
		t.Errorf("steps should be renumbered 1..n, got %d and %d",
			saved.Steps[0].Order, saved.Steps[1].Order)
	}
}

func TestActivatingWithNoEmbedderLeavesTheSkillPending(t *testing.T) {
	m, _ := newManager(t, &bagOfWords{fail: errors.New("connection refused")})

	sk := create(t, m, sample("Payment triage", "the payment service is slow", "read the metrics"))
	if sk.Status != store.SkillActive {
		t.Fatalf("the skill should still be created: %+v", sk)
	}
	if sk.HasVector {
		t.Error("nothing could have embedded it")
	}

	stats, err := m.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Active != 1 || stats.Pending != 1 {
		t.Fatalf("the library should report one pending skill: %+v", stats)
	}

	// Once the runtime is back, one call fixes the library.
	m.SetEmbedder(&bagOfWords{})
	if err := m.EnsureVectors(context.Background()); err != nil {
		t.Fatalf("ensure vectors: %v", err)
	}
	if stats, _ := m.Stats(); stats.Pending != 0 {
		t.Errorf("nothing should be pending after embedding: %+v", stats)
	}
	if got, _ := m.Match(context.Background(), skill.Query{Text: "payment is slow", TopK: 5}); len(got) != 1 {
		t.Error("the skill should be matchable once embedded")
	}
}
