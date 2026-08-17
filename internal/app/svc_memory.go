package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"agentmux/internal/memory"
	"agentmux/internal/store"
)

// MemoryService exposes the memory library to the frontend.
type MemoryService struct {
	core *Core

	// reindexMu makes the rebuild single-flight. Two concurrent rebuilds would
	// embed the same rows twice and race on the same table for no gain.
	reindexMu sync.Mutex
	reindexOn bool
	cancel    context.CancelFunc
}

// NewMemoryService binds a memory service to the core.
func NewMemoryService(c *Core) *MemoryService { return &MemoryService{core: c} }

// ServiceName identifies the service in Wails logs.
func (m *MemoryService) ServiceName() string { return "MemoryService" }

// List returns stored memories, newest first.
func (m *MemoryService) List(f store.MemoryFilter) ([]store.Memory, error) {
	if f.Limit <= 0 {
		f.Limit = 200
	}
	return m.core.Store.ListMemories(f)
}

// Count reports how many memories match a filter, which is what tells the user
// their search box is hiding things rather than the library being empty.
func (m *MemoryService) Count(f store.MemoryFilter) (int, error) {
	return m.core.Store.CountMemories(f)
}

// Search finds memories by meaning. It needs the embedder, so it fails loudly
// when the runtime is down rather than quietly returning nothing — an empty
// result and an unreachable model look identical to a user otherwise.
func (m *MemoryService) Search(q memory.Query) ([]memory.Hit, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return m.core.Memory.Search(ctx, q)
}

// Add writes one memory by hand.
func (m *MemoryService) Add(in store.Memory) (memory.PutResult, error) {
	if strings.TrimSpace(in.Body) == "" {
		return memory.PutResult{}, errors.New("a memory needs a body")
	}
	if in.Source == "" {
		in.Source = "user"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return m.core.Memory.Put(ctx, in)
}

// Delete removes one memory.
func (m *MemoryService) Delete(id string) error { return m.core.Memory.Delete(id) }

// Stats summarises the library and whether it needs rebuilding.
func (m *MemoryService) Stats() (memory.Stats, error) { return m.core.Memory.Stats() }

// ReindexStatus is the state of a rebuild.
type ReindexStatus struct {
	Running bool   `json:"running"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Error   string `json:"error"`
}

// Reindex embeds every memory that has no vector for the current model.
//
// It returns as soon as the work starts and reports progress through
// memory:reindex events, because embedding thousands of rows on a CPU takes
// minutes and a frozen window for the duration is not an option.
func (m *MemoryService) Reindex() error {
	m.reindexMu.Lock()
	if m.reindexOn {
		m.reindexMu.Unlock()
		return errors.New("a rebuild is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.reindexOn = true
	m.cancel = cancel
	m.reindexMu.Unlock()

	go func() {
		defer cancel()
		err := m.core.Memory.Reindex(ctx, func(done, total int) {
			m.core.Emit("memory:reindex", ReindexStatus{Running: true, Done: done, Total: total})
		})

		m.reindexMu.Lock()
		m.reindexOn = false
		m.cancel = nil
		m.reindexMu.Unlock()

		status := ReindexStatus{}
		if err != nil && !errors.Is(err, context.Canceled) {
			status.Error = err.Error()
		}
		m.core.Emit("memory:reindex", status)
	}()
	return nil
}

// CancelReindex stops a running rebuild. Rows already embedded keep their
// vectors, so cancelling loses progress rather than work.
func (m *MemoryService) CancelReindex() {
	m.reindexMu.Lock()
	defer m.reindexMu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
}
