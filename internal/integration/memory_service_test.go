package integration

import (
	"strings"
	"testing"

	"agentmux/internal/app"
	"agentmux/internal/memory"
	"agentmux/internal/store"
)

// newCore builds the real application core against a throwaway data directory.
// Nothing here needs a network: the whole point is that the memory library and
// the settings screen work on a machine with no model runtime installed.
func newCore(t *testing.T) *app.Core {
	t.Helper()
	t.Setenv("AGENTMUX_DATA_DIR", t.TempDir())

	core, err := app.NewCore()
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	t.Cleanup(core.Shutdown)
	return core
}

// TestMemorySurvivesWithoutAModelRuntime is the offline path end to end: a
// memory written while nothing is listening is stored, is listed, and is
// honestly reported as not searchable rather than silently lost.
func TestMemorySurvivesWithoutAModelRuntime(t *testing.T) {
	core := newCore(t)
	llmSvc := app.NewLLMService(core)
	memSvc := app.NewMemoryService(core)

	// Point at a port nothing listens on, so the result does not depend on
	// whether the developer running the tests happens to have Ollama up.
	if _, err := llmSvc.SaveConfig(app.LLMConfig{
		BaseURL: "http://127.0.0.1:1", ChatModel: "qwen3:8b", EmbedModel: "bge-m3",
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}

	status := llmSvc.Status()
	if status.Reachable {
		t.Fatal("nothing should be reachable on port 1")
	}
	if !strings.Contains(status.Hint, "ollama serve") {
		t.Errorf("the status should say how to start the runtime, got %q", status.Hint)
	}

	res, err := memSvc.Add(store.Memory{
		Kind:  store.MemUserPref,
		Title: "Deploys",
		Body:  "always deploy from the release branch, never from main",
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if res.EmbedError == "" {
		t.Error("the unreachable embedder should have been reported")
	}
	if res.Memory.ID == "" {
		t.Fatal("the memory was not stored")
	}

	rows, err := memSvc.List(store.MemoryFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || !strings.Contains(rows[0].Body, "release branch") {
		t.Fatalf("expected the memory to be listed, got %+v", rows)
	}

	stats, err := memSvc.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Total != 1 || stats.Pending != 1 || !stats.NeedsRebuild {
		t.Errorf("the library should report one pending memory: %+v", stats)
	}

	// Searching has to fail loudly. Returning an empty list would be
	// indistinguishable from "nothing was ever remembered".
	if _, err := memSvc.Search(memoryQuery("release branch")); err == nil {
		t.Error("semantic search should fail when the embedder is unreachable")
	}

	if err := memSvc.Delete(res.Memory.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n, _ := memSvc.Count(store.MemoryFilter{}); n != 0 {
		t.Errorf("after deleting, the library should be empty, got %d", n)
	}
}

// TestMemoryTextFilterIsLiteral guards the search box against a user typing a
// character SQL treats as a wildcard.
func TestMemoryTextFilterIsLiteral(t *testing.T) {
	core := newCore(t)
	memSvc := app.NewMemoryService(core)

	for _, body := range []string{"disk is 100% full", "disk is fine"} {
		if _, err := memSvc.Add(store.Memory{Kind: store.MemSystemLog, Body: body}); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	rows, err := memSvc.List(store.MemoryFilter{Text: "100%"})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("searching for \"100%%\" matched %d rows, want 1", len(rows))
	}
}

// memoryQuery keeps the test readable; the service takes the full query shape.
func memoryQuery(text string) memory.Query {
	return memory.Query{Text: text, TopK: 5}
}
