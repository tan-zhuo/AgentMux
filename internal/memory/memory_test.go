package memory_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"agentmux/internal/memory"
	"agentmux/internal/store"
)

// fakeEmbedder turns text into a vector without a model, so these tests
// exercise the indexing and retrieval logic rather than Ollama.
//
// The vector is a bag of words over a fixed vocabulary: two texts sharing words
// score high, texts sharing none score zero. That is enough structure to assert
// on ranking, which is the property that matters here.
type fakeEmbedder struct {
	mu     sync.Mutex
	calls  int
	texts  []string
	fail   error
	stride int // set to change the dimension, to simulate a model swap
}

var vocab = []string{
	"payment", "latency", "database", "deploy", "restart",
	"cache", "index", "timeout", "memory", "agent",
}

func (f *fakeEmbedder) Embed(_ context.Context, _ string, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	f.calls++
	f.texts = append(f.texts, texts...)

	dim := len(vocab)
	if f.stride > 0 {
		dim = f.stride
	}
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v := make([]float32, dim)
		lower := strings.ToLower(t)
		for j, w := range vocab {
			if j >= dim {
				break
			}
			if strings.Contains(lower, w) {
				v[j] = 1
			}
		}
		// A non-zero floor keeps a text with no vocabulary words from being a
		// zero vector, which cannot be normalised and would never match.
		v[dim-1] += 0.01
		out[i] = v
	}
	return out, nil
}

func newIndex(t *testing.T, emb memory.Embedder, model string) (*memory.Index, *store.Store) {
	t.Helper()
	t.Setenv("AGENTMUX_DATA_DIR", t.TempDir())

	st, err := store.Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	return memory.NewIndex(st, emb, func() string { return model }), st
}

func put(t *testing.T, ix *memory.Index, title, body string) store.Memory {
	t.Helper()
	res, err := ix.Put(context.Background(), store.Memory{
		Kind:  store.MemProjectFact,
		Title: title,
		Body:  body,
	})
	if err != nil {
		t.Fatalf("put %q: %v", title, err)
	}
	if res.EmbedError != "" {
		t.Fatalf("put %q: embed: %s", title, res.EmbedError)
	}
	return res.Memory
}

func TestSearchRanksBySimilarity(t *testing.T) {
	ix, _ := newIndex(t, &fakeEmbedder{}, "fake-v1")

	put(t, ix, "Payment latency", "The payment service has a latency problem under load")
	put(t, ix, "Deploy steps", "Deploy is a restart of the agent process")
	put(t, ix, "Database index", "Adding a database index fixed the timeout")

	hits, err := ix.Search(context.Background(), memory.Query{
		Text: "why is payment slow, latency", TopK: 2,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("expected at least one hit")
	}
	if !strings.Contains(hits[0].Memory.Title, "Payment") {
		t.Errorf("expected the payment memory first, got %q", hits[0].Memory.Title)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Errorf("results are not sorted: %v then %v", hits[i-1].Score, hits[i].Score)
		}
	}
}

func TestSearchRespectsScopeFilter(t *testing.T) {
	ix, _ := newIndex(t, &fakeEmbedder{}, "fake-v1")

	_, err := ix.Put(context.Background(), store.Memory{
		Kind: store.MemProjectFact, Scope: store.ScopeGlobal,
		Title: "Global cache note", Body: "the cache is shared",
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	// A project-scoped memory with no project id would be unreachable through
	// the project filter, which is the point being asserted below.
	if _, err := ix.Put(context.Background(), store.Memory{
		Kind: store.MemProjectFact, Scope: store.ScopeGlobal,
		Title: "Another cache note", Body: "the cache needs a restart",
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	hits, err := ix.Search(context.Background(), memory.Query{
		Text: "cache", Scope: string(store.ScopeAgent), TopK: 5,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("scope filter let %d memories through", len(hits))
	}
}

func TestPutKeepsMemoryWhenTheEmbedderIsDown(t *testing.T) {
	emb := &fakeEmbedder{fail: errors.New("connection refused")}
	ix, st := newIndex(t, emb, "fake-v1")

	res, err := ix.Put(context.Background(), store.Memory{
		Kind: store.MemUserPref, Title: "Prefers tmux", Body: "always attach rather than restart",
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if res.EmbedError == "" {
		t.Error("expected the embedding failure to be reported")
	}
	if res.Memory.HasVector {
		t.Error("a memory with no embedding should not claim to have a vector")
	}

	// The text has to survive: losing it because a separate process was down
	// would be the worst possible outcome of a transient failure.
	rows, err := st.ListMemories(store.MemoryFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Body != "always attach rather than restart" {
		t.Fatalf("memory was not stored: %+v", rows)
	}

	// And it has to be picked up once the embedder is back.
	emb.mu.Lock()
	emb.fail = nil
	emb.mu.Unlock()

	if err := ix.Reindex(context.Background(), nil); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	hits, err := ix.Search(context.Background(), memory.Query{Text: "restart", TopK: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected the reindexed memory to be findable, got %d hits", len(hits))
	}
}

// TestChangingTheModelStrandsOldVectors is the regression test for the silent
// failure that motivated storing the model name: vectors from another model are
// not comparable, and mixing them returns confident nonsense instead of an
// error.
func TestChangingTheModelStrandsOldVectors(t *testing.T) {
	t.Setenv("AGENTMUX_DATA_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	model := "fake-v1"
	emb := &fakeEmbedder{}
	ix := memory.NewIndex(st, emb, func() string { return model })

	put(t, ix, "Payment latency", "the payment service is slow")

	stats, err := ix.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Embedded != 1 || stats.Pending != 0 {
		t.Fatalf("before the swap: embedded=%d pending=%d", stats.Embedded, stats.Pending)
	}

	// Swap the model, as the settings screen does.
	model = "fake-v2"
	emb.stride = 6
	ix.Invalidate()

	stats, err = ix.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Pending != 1 || !stats.NeedsRebuild {
		t.Fatalf("after the swap the library should need a rebuild: %+v", stats)
	}

	// The stranded vector must not be compared against the new model's output.
	hits, err := ix.Search(context.Background(), memory.Query{Text: "payment", TopK: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("vectors from the old model were still searched: %d hits", len(hits))
	}

	if err := ix.Reindex(context.Background(), nil); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	stats, err = ix.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Pending != 0 || stats.Embedded != 1 {
		t.Fatalf("after the rebuild: %+v", stats)
	}
	hits, err = ix.Search(context.Background(), memory.Query{Text: "payment", TopK: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected the rebuilt memory to be findable, got %d", len(hits))
	}
}

func TestReindexReportsProgress(t *testing.T) {
	ix, _ := newIndex(t, &fakeEmbedder{fail: errors.New("down")}, "fake-v1")

	for i := range 20 {
		if _, err := ix.Put(context.Background(), store.Memory{
			Kind: store.MemAgentEvent, Body: "event " + string(rune('a'+i)),
		}); err != nil {
			t.Fatalf("put: %v", err)
		}
	}

	ix.SetEmbedder(&fakeEmbedder{})
	var last int
	if err := ix.Reindex(context.Background(), func(done, total int) {
		if total != 20 {
			t.Errorf("total should be 20, got %d", total)
		}
		if done <= last {
			t.Errorf("progress went backwards: %d after %d", done, last)
		}
		last = done
	}); err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if last != 20 {
		t.Errorf("final progress was %d, want 20", last)
	}
}

func TestPutRedactsSecrets(t *testing.T) {
	ix, st := newIndex(t, &fakeEmbedder{}, "fake-v1")

	if _, err := ix.Put(context.Background(), store.Memory{
		Kind: store.MemSystemLog,
		Body: "deploy failed, used token ghp_abcdefghijklmnopqrstuvwxyz012345 to clone",
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	rows, err := st.ListMemories(store.MemoryFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if strings.Contains(rows[0].Body, "ghp_abcdefghijklmnopqrstuvwxyz012345") {
		t.Error("the token reached the database")
	}
	if !rows[0].Redacted {
		t.Error("the memory should be marked as redacted")
	}
}

func TestVectorRoundTrip(t *testing.T) {
	in := []float32{0, 1, -1, 0.5, float32(math.Pi)}
	out, err := store.DecodeVector(store.EncodeVector(in))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("length changed: %d -> %d", len(in), len(out))
	}
	for i := range in {
		if in[i] != out[i] {
			t.Errorf("element %d: %v -> %v", i, in[i], out[i])
		}
	}

	if _, err := store.DecodeVector([]byte{1, 2, 3}); err == nil {
		t.Error("a truncated blob should be an error, not a short vector")
	}
}

func TestNormaliseMakesUnitVectors(t *testing.T) {
	v := memory.Normalise([]float32{3, 4})
	var sum float64
	for _, f := range v {
		sum += float64(f) * float64(f)
	}
	if math.Abs(sum-1) > 1e-6 {
		t.Errorf("length squared is %v, want 1", sum)
	}

	// A zero vector has no direction; it must survive rather than become NaN,
	// because NaN would silently poison every comparison it takes part in.
	z := memory.Normalise([]float32{0, 0})
	for i, f := range z {
		if math.IsNaN(float64(f)) {
			t.Errorf("element %d became NaN", i)
		}
	}
}
