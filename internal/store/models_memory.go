package store

// MemoryKind classifies what a memory is about. The layering follows the spec:
// facts about a project, things an agent did, preferences the user stated,
// context from a session, and the orchestrator's own record of what it ran.
type MemoryKind string

const (
	MemProjectFact MemoryKind = "project_fact"
	MemAgentEvent  MemoryKind = "agent_event"
	MemUserPref    MemoryKind = "user_pref"
	MemSessionCtx  MemoryKind = "session_ctx"
	MemSystemLog   MemoryKind = "system_log"
)

// MemoryScope limits which situations a memory is retrieved in.
type MemoryScope string

const (
	ScopeGlobal  MemoryScope = "global"
	ScopeProject MemoryScope = "project"
	ScopeAgent   MemoryScope = "agent"
)

// Memory is one remembered fact.
//
// The embedding is deliberately not part of the JSON surface: a thousand floats
// per row would dwarf the text they describe on every list call, and the
// frontend has no use for them.
type Memory struct {
	ID         string      `json:"id"`
	Kind       MemoryKind  `json:"kind"`
	Scope      MemoryScope `json:"scope"`
	ProjectID  string      `json:"projectId"`
	AgentID    string      `json:"agentId"`
	ServerID   string      `json:"serverId"`
	Title      string      `json:"title"`
	Body       string      `json:"body"`
	Redacted   bool        `json:"redacted"`
	Source     string      `json:"source"`
	Importance float64     `json:"importance"`

	EmbeddingModel string `json:"embeddingModel"`
	Dim            int    `json:"dim"`
	// HasVector says whether this row can be found by semantic search at all.
	HasVector bool `json:"hasVector"`

	CreatedAt  int64  `json:"createdAt"`
	LastUsedAt *int64 `json:"lastUsedAt"`
	UseCount   int    `json:"useCount"`

	// Embedding is loaded only by the paths that need it.
	Embedding []float32 `json:"-"`
}

// MemoryFilter narrows a listing. Zero values mean "no constraint", so the
// empty filter lists everything.
type MemoryFilter struct {
	Kinds     []string `json:"kinds"`
	Scope     string   `json:"scope"`
	ProjectID string   `json:"projectId"`
	AgentID   string   `json:"agentId"`
	// Text is a plain substring match over title and body, for the search box.
	// Semantic search is a different call; this one is for finding a memory you
	// already know the wording of.
	Text   string `json:"text"`
	Limit  int    `json:"limit"`
	Offset int    `json:"offset"`
}

// VectorSpace counts the rows sharing one embedding model and dimension.
//
// More than one row here means the embedding model was changed and part of the
// library is unreachable by search until it is rebuilt — the failure the UI has
// to surface, because nothing else about it looks broken.
type VectorSpace struct {
	Model string `json:"model"`
	Dim   int    `json:"dim"`
	Count int    `json:"count"`
}
