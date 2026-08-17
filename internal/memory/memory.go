// Package memory stores what the orchestrator has learned and finds it again by
// meaning rather than by wording.
//
// Vectors live in the same SQLite database as everything else and are compared
// by scanning them all. That sounds naive and is not: with the ten thousand
// memories a desktop realistically accumulates, one full scan of 1024-dimension
// vectors takes about ten milliseconds, while the local model that consumes the
// result takes hundreds of milliseconds to produce its first token. An
// approximate index would add a dependency, a build step and a class of
// wrong-answer bugs in exchange for time nobody can perceive.
//
// The Index interface is shaped so that swapping in HNSW later touches nothing
// above it — the point at which that becomes worth doing is somewhere past
// 100k memories, where the scan reaches ~100ms.
package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"agentmux/internal/store"
)

// Embedder is the part of the LLM client this package needs. Depending on the
// method rather than the client keeps tests free of HTTP.
type Embedder interface {
	Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// Query is a semantic search.
type Query struct {
	Text      string   `json:"text"`
	Scope     string   `json:"scope"`
	ProjectID string   `json:"projectId"`
	AgentID   string   `json:"agentId"`
	Kinds     []string `json:"kinds"`
	TopK      int      `json:"topK"`
	MinScore  float32  `json:"minScore"`
}

// Hit is one search result.
type Hit struct {
	Memory store.Memory `json:"memory"`
	Score  float32      `json:"score"`
}

// PutResult is the outcome of writing a memory.
//
// EmbedError is not an error return because a memory that could not be embedded
// is still a memory worth keeping — the embedder is a separate process that may
// simply not be running. It comes back as text so the UI can say what happened
// instead of silently storing something that will never be found.
type PutResult struct {
	Memory     store.Memory `json:"memory"`
	EmbedError string       `json:"embedError"`
}

// Stats describes the state of the library.
type Stats struct {
	Total        int                 `json:"total"`
	Embedded     int                 `json:"embedded"`
	Pending      int                 `json:"pending"`
	Model        string              `json:"model"`
	Spaces       []store.VectorSpace `json:"spaces"`
	NeedsRebuild bool                `json:"needsRebuild"`
}

// Index is the vector store over the memories table.
type Index struct {
	store *store.Store
	emb   Embedder
	// model is read through a function because the user can change it in
	// Settings at any moment, and a cached copy here would keep embedding
	// against the previous one until restart.
	model func() string

	mu     sync.RWMutex
	space  string // the model the cache was built for
	dim    int
	vecs   []store.VectorRow
	loaded bool
}

// NewIndex builds an index over a store.
func NewIndex(st *store.Store, emb Embedder, model func() string) *Index {
	return &Index{store: st, emb: emb, model: model}
}

// SetEmbedder swaps the client, which happens when the base URL changes.
func (ix *Index) SetEmbedder(emb Embedder) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.emb = emb
}

// Invalidate drops the cached vectors, so the next search reloads them.
func (ix *Index) Invalidate() {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.loaded = false
	ix.vecs = nil
}

// embedOne returns a normalised vector for a single text.
func (ix *Index) embedOne(ctx context.Context, text string) ([]float32, error) {
	ix.mu.RLock()
	emb := ix.emb
	ix.mu.RUnlock()
	if emb == nil {
		return nil, fmt.Errorf("no embedder configured")
	}
	vecs, err := emb.Embed(ctx, ix.model(), []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("embedder returned nothing for a one-item batch")
	}
	return Normalise(vecs[0]), nil
}

// embedText is what actually gets vectorised: the title carries the topic and
// the body carries the detail, and searching by topic alone is how most
// retrieval starts.
func embedText(m store.Memory) string {
	if m.Title == "" {
		return m.Body
	}
	return m.Title + "\n" + m.Body
}

// Put redacts, embeds and stores one memory.
func (ix *Index) Put(ctx context.Context, m store.Memory) (PutResult, error) {
	body, bodyHit := Redact(m.Body)
	title, titleHit := Redact(m.Title)
	m.Body, m.Title = body, title
	m.Redacted = bodyHit || titleHit

	var res PutResult
	if vec, err := ix.embedOne(ctx, embedText(m)); err != nil {
		res.EmbedError = err.Error()
	} else {
		m.Embedding = vec
		m.EmbeddingModel = ix.model()
	}

	saved, err := ix.store.InsertMemory(m)
	if err != nil {
		return PutResult{}, err
	}
	res.Memory = saved

	if len(m.Embedding) > 0 {
		ix.mu.Lock()
		if ix.loaded && ix.space == saved.EmbeddingModel && ix.dim == len(m.Embedding) {
			ix.vecs = append(ix.vecs, store.VectorRow{ID: saved.ID, Vec: m.Embedding})
		}
		ix.mu.Unlock()
	}
	return res, nil
}

// Delete removes a memory and drops it from the cache.
func (ix *Index) Delete(id string) error {
	if err := ix.store.DeleteMemory(id); err != nil {
		return err
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for i, v := range ix.vecs {
		if v.ID == id {
			ix.vecs = append(ix.vecs[:i], ix.vecs[i+1:]...)
			break
		}
	}
	return nil
}

// ensureLoaded fills the vector cache for the current model.
//
// Reading 30MB of blobs out of SQLite costs more than the arithmetic it feeds,
// so it happens once rather than per search. The cache is keyed by model: a
// change in Settings makes the next search reload rather than compare vectors
// from two different spaces, which would return confident nonsense.
func (ix *Index) ensureLoaded() error {
	model := ix.model()

	ix.mu.RLock()
	ok := ix.loaded && ix.space == model
	ix.mu.RUnlock()
	if ok {
		return nil
	}

	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.loaded && ix.space == model {
		return nil
	}

	// The dimension is whichever one the rows in this space already use; there
	// is no need to know it in advance, and hard-coding it per model would be
	// one more thing to keep true.
	spaces, err := ix.store.VectorSpaces()
	if err != nil {
		return err
	}
	dim := 0
	for _, s := range spaces {
		if s.Model == model && s.Count > 0 {
			dim = s.Dim
			break
		}
	}
	if dim == 0 {
		ix.vecs, ix.dim, ix.space, ix.loaded = nil, 0, model, true
		return nil
	}

	rows, err := ix.store.LoadVectors(model, dim)
	if err != nil {
		return err
	}
	ix.vecs, ix.dim, ix.space, ix.loaded = rows, dim, model, true
	return nil
}

// Search returns the memories closest in meaning to the query text.
func (ix *Index) Search(ctx context.Context, q Query) ([]Hit, error) {
	if strings.TrimSpace(q.Text) == "" {
		return []Hit{}, nil
	}
	if q.TopK <= 0 {
		q.TopK = 8
	}
	if err := ix.ensureLoaded(); err != nil {
		return nil, err
	}

	qv, err := ix.embedOne(ctx, q.Text)
	if err != nil {
		return nil, err
	}

	// Metadata filtering happens in SQL, similarity in Go. The filter is what
	// keeps another project's memories out of this project's planning, so it
	// runs first and narrows what is scored.
	var allowed map[string]bool
	filter := store.MemoryFilter{
		Kinds: q.Kinds, Scope: q.Scope, ProjectID: q.ProjectID, AgentID: q.AgentID,
	}
	if filter.Scope != "" || filter.ProjectID != "" || filter.AgentID != "" || len(filter.Kinds) > 0 {
		ids, err := ix.store.MemoryIDs(filter)
		if err != nil {
			return nil, err
		}
		allowed = make(map[string]bool, len(ids))
		for _, id := range ids {
			allowed[id] = true
		}
		if len(allowed) == 0 {
			return []Hit{}, nil
		}
	}

	ix.mu.RLock()
	vecs := ix.vecs
	dim := ix.dim
	ix.mu.RUnlock()

	if dim != 0 && len(qv) != dim {
		return nil, fmt.Errorf(
			"the query embedding has %d dimensions but the stored memories have %d — "+
				"the embedding model changed; rebuild the index", len(qv), dim)
	}

	best := newTopK(q.TopK)
	for _, row := range vecs {
		if allowed != nil && !allowed[row.ID] {
			continue
		}
		if s := dot(qv, row.Vec); s >= q.MinScore {
			best.add(row.ID, s)
		}
	}

	results := best.results()
	hits := make([]Hit, 0, len(results))
	ids := make([]string, 0, len(results))
	for _, r := range results {
		m, err := ix.store.GetMemory(r.id)
		if err != nil {
			// A row deleted between the scan and the fetch is not an error
			// worth failing the search over.
			continue
		}
		hits = append(hits, Hit{Memory: m, Score: r.score})
		ids = append(ids, r.id)
	}
	_ = ix.store.TouchMemories(ids)
	return hits, nil
}

// reindexBatch is small enough that a slow CPU still reports progress often,
// and large enough that the per-request overhead does not dominate.
const reindexBatch = 16

// Reindex embeds everything that has no usable vector in the current space.
//
// This is the recovery path for two situations: memories written while Ollama
// was down, and a change of embedding model — which silently invalidates every
// existing vector, since numbers from two models are not comparable.
func (ix *Index) Reindex(ctx context.Context, progress func(done, total int)) error {
	model := ix.model()
	if model == "" {
		return fmt.Errorf("no embedding model configured")
	}

	ix.mu.RLock()
	emb := ix.emb
	ix.mu.RUnlock()
	if emb == nil {
		return fmt.Errorf("no embedder configured")
	}

	pending, err := ix.store.MemoriesToEmbed(model, 0)
	if err != nil {
		return err
	}
	total := len(pending)
	if total == 0 {
		if progress != nil {
			progress(0, 0)
		}
		return nil
	}

	done := 0
	for start := 0; start < len(pending); start += reindexBatch {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+reindexBatch, len(pending))
		batch := pending[start:end]

		texts := make([]string, len(batch))
		for i, m := range batch {
			texts[i] = embedText(m)
		}
		vecs, err := emb.Embed(ctx, model, texts)
		if err != nil {
			return fmt.Errorf("embedding %d of %d: %w", done, total, err)
		}
		if len(vecs) != len(batch) {
			return fmt.Errorf("asked for %d embeddings, got %d", len(batch), len(vecs))
		}
		for i, m := range batch {
			if err := ix.store.SetMemoryVector(m.ID, Normalise(vecs[i]), model); err != nil {
				return err
			}
		}
		done += len(batch)
		if progress != nil {
			progress(done, total)
		}
	}

	ix.Invalidate()
	return nil
}

// Stats summarises the library, including whether part of it is stranded in an
// old embedding space.
func (ix *Index) Stats() (Stats, error) {
	model := ix.model()
	st := Stats{Model: model}

	total, err := ix.store.CountMemories(store.MemoryFilter{})
	if err != nil {
		return Stats{}, err
	}
	st.Total = total

	spaces, err := ix.store.VectorSpaces()
	if err != nil {
		return Stats{}, err
	}
	st.Spaces = spaces
	for _, s := range spaces {
		if s.Model == model {
			st.Embedded += s.Count
		}
	}
	st.Pending = total - st.Embedded
	st.NeedsRebuild = st.Pending > 0
	return st, nil
}
