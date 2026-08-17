// Package skill manages reusable decision procedures: their content, their
// lifecycle, their history, and which of them apply to a situation.
//
// Two rules shape everything here.
//
// A skill only ever recommends. It names tools; it never carries anything
// executable. Whether a named tool actually runs is decided elsewhere, by the
// gate, against the tool's own risk level — so a skill cannot become a way
// around the permissions, however it was written or whoever wrote it.
//
// Only an active skill is embedded. A draft has no vector at all, so it cannot
// be matched, so it cannot influence a plan before a person has approved it.
// Enforcing that by absence rather than by a filter means a forgotten condition
// somewhere cannot quietly reintroduce unreviewed advice.
package skill

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"agentmux/internal/memory"
	"agentmux/internal/store"
)

// Embedder is the part of the LLM client this package needs.
type Embedder interface {
	Embed(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// Manager owns the skill library.
type Manager struct {
	store *store.Store
	emb   Embedder
	model func() string
}

// NewManager builds a manager over a store.
func NewManager(st *store.Store, emb Embedder, model func() string) *Manager {
	return &Manager{store: st, emb: emb, model: model}
}

// SetEmbedder swaps the client when the runtime address changes.
func (m *Manager) SetEmbedder(emb Embedder) { m.emb = emb }

// List returns matching skills.
func (m *Manager) List(f store.SkillFilter) ([]store.Skill, error) {
	return m.store.ListSkills(f)
}

// Get loads one skill.
func (m *Manager) Get(id string) (store.Skill, error) { return m.store.GetSkill(id) }

// Create validates and stores a new skill.
//
// A skill created by a person may start active — they wrote it, that is the
// approval. One proposed by the orchestrator arrives as a draft and stays there
// until someone says otherwise.
func (m *Manager) Create(ctx context.Context, sk store.Skill) (store.Skill, error) {
	if err := Validate(&sk); err != nil {
		return store.Skill{}, err
	}
	if sk.Status == "" {
		sk.Status = store.SkillActive
	}
	if sk.CreatedBy == "orchestrator" {
		sk.Status = store.SkillDraft
	}
	if err := validStatus(sk.Status); err != nil {
		return store.Skill{}, err
	}

	saved, err := m.store.InsertSkill(sk)
	if err != nil {
		return store.Skill{}, err
	}
	if err := m.store.AddSkillVersion(saved, "created", saved.CreatedBy); err != nil {
		return store.Skill{}, err
	}
	if saved.Status == store.SkillActive {
		_ = m.embed(ctx, saved)
	}
	return m.store.GetSkill(saved.ID)
}

// Update replaces the content of a skill, keeping the old version.
func (m *Manager) Update(ctx context.Context, sk store.Skill, note string) (store.Skill, error) {
	if err := Validate(&sk); err != nil {
		return store.Skill{}, err
	}
	current, err := m.store.GetSkill(sk.ID)
	if err != nil {
		return store.Skill{}, err
	}

	// The snapshot is of what is being replaced, taken before it is gone.
	if err := m.store.AddSkillVersion(current, note, "user"); err != nil {
		return store.Skill{}, err
	}

	sk.Version = current.Version + 1
	sk.Status = current.Status
	sk.CreatedBy = current.CreatedBy
	sk.OriginRunID = current.OriginRunID

	saved, err := m.store.UpdateSkill(sk)
	if err != nil {
		return store.Skill{}, err
	}
	// UpdateSkill drops the vector, because the trigger may have changed and a
	// stale vector would go on matching the old wording.
	if saved.Status == store.SkillActive {
		_ = m.embed(ctx, saved)
	}
	return m.store.GetSkill(saved.ID)
}

// Delete removes a skill outright, history included.
//
// Archiving is the usual answer; this is for a skill that should never have
// existed, where keeping the record is worse than losing it.
func (m *Manager) Delete(id string) error { return m.store.DeleteSkill(id) }

// --- lifecycle --------------------------------------------------------------

// Event is something a person does to a skill.
type Event string

const (
	Approve Event = "approve"
	Reject  Event = "reject"
	Disable Event = "disable"
	Enable  Event = "enable"
	Archive Event = "archive"
	Restore Event = "restore"
)

// transitions is the state machine. A pair absent from this table is not a
// missing feature; it is a move that does not exist.
var transitions = map[store.SkillStatus]map[Event]store.SkillStatus{
	store.SkillDraft: {
		Approve: store.SkillActive,
		Reject:  store.SkillRejected,
	},
	store.SkillActive: {
		Disable: store.SkillDisabled,
		Archive: store.SkillArchived,
	},
	store.SkillDisabled: {
		Enable:  store.SkillActive,
		Archive: store.SkillArchived,
	},
	store.SkillArchived: {
		Restore: store.SkillActive,
	},
}

func validStatus(s store.SkillStatus) error {
	switch s {
	case store.SkillDraft, store.SkillActive, store.SkillDisabled,
		store.SkillArchived, store.SkillRejected:
		return nil
	}
	return fmt.Errorf("%q is not a status", s)
}

// Apply moves a skill through the state machine.
//
// Entering active embeds the skill; leaving it drops the vector, which is what
// removes the skill from matching immediately rather than at the next rebuild.
func (m *Manager) Apply(ctx context.Context, id string, ev Event) (store.Skill, error) {
	sk, err := m.store.GetSkill(id)
	if err != nil {
		return store.Skill{}, err
	}
	next, ok := transitions[sk.Status][ev]
	if !ok {
		return store.Skill{}, fmt.Errorf("a %s skill cannot be %sd", sk.Status, ev)
	}

	if err := m.store.SetSkillStatus(id, next); err != nil {
		return store.Skill{}, err
	}
	updated, err := m.store.GetSkill(id)
	if err != nil {
		return store.Skill{}, err
	}

	if next == store.SkillActive {
		_ = m.embed(ctx, updated)
	} else if err := m.store.ClearSkillVector(id); err != nil {
		return store.Skill{}, err
	}
	return m.store.GetSkill(id)
}

// Rollback restores the content of an earlier version as a new version.
//
// The history is append-only: rolling back adds, it does not erase. Undoing a
// rollback is therefore another rollback, and the record of what was tried
// stays intact.
func (m *Manager) Rollback(ctx context.Context, id string, version int) (store.Skill, error) {
	current, err := m.store.GetSkill(id)
	if err != nil {
		return store.Skill{}, err
	}
	old, err := m.store.GetSkillVersion(id, version)
	if err != nil {
		return store.Skill{}, err
	}

	if err := m.store.AddSkillVersion(current,
		fmt.Sprintf("replaced by a rollback to v%d", version), "user"); err != nil {
		return store.Skill{}, err
	}

	restored := old.Snapshot
	restored.ID = current.ID
	restored.Version = current.Version + 1
	restored.Status = current.Status
	restored.CreatedAt = current.CreatedAt
	restored.UsageCount = current.UsageCount
	restored.LastUsedAt = current.LastUsedAt
	restored.SuccessCount = current.SuccessCount
	restored.FailureCount = current.FailureCount

	saved, err := m.store.UpdateSkill(restored)
	if err != nil {
		return store.Skill{}, err
	}
	if saved.Status == store.SkillActive {
		_ = m.embed(ctx, saved)
	}
	return m.store.GetSkill(id)
}

// Versions returns the history of one skill.
func (m *Manager) Versions(id string) ([]store.SkillVersion, error) {
	return m.store.ListSkillVersions(id)
}

// --- matching ---------------------------------------------------------------

// Match is one skill that applies to a situation.
type Match struct {
	Skill store.Skill `json:"skill"`
	Score float32     `json:"score"`
	// Reason says why this one surfaced, in the terms the match was actually
	// made on. Planning that cannot be questioned cannot be corrected, and a
	// bare number is not something a person can argue with.
	Reason string `json:"reason"`
}

// Query is a situation to match skills against.
type Query struct {
	Text      string  `json:"text"`
	ProjectID string  `json:"projectId"`
	AgentType string  `json:"agentType"`
	TopK      int     `json:"topK"`
	MinScore  float32 `json:"minScore"`
}

// Match finds the skills that apply to a situation.
func (m *Manager) Match(ctx context.Context, q Query) ([]Match, error) {
	if strings.TrimSpace(q.Text) == "" {
		return []Match{}, nil
	}
	if q.TopK <= 0 {
		q.TopK = 5
	}
	if q.MinScore == 0 {
		// Below this, a "match" is two texts that merely share a language.
		q.MinScore = 0.3
	}

	qv, err := m.embedOne(ctx, q.Text)
	if err != nil {
		return nil, err
	}

	rows, err := m.store.ActiveSkillVectors(m.model(), len(qv))
	if err != nil {
		return nil, err
	}

	out := []Match{}
	for _, row := range rows {
		score := memory.Dot(qv, row.Vec)
		if score < q.MinScore {
			continue
		}
		sk, err := m.store.GetSkill(row.ID)
		if err != nil {
			continue
		}
		if !applies(sk, q) {
			continue
		}
		out = append(out, Match{Skill: sk, Score: score, Reason: reason(sk, score)})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > q.TopK {
		out = out[:q.TopK]
	}
	return out, nil
}

// applies checks the scope, which is a hard filter rather than a score: a
// skill written for one project is wrong elsewhere no matter how similar the
// wording is.
func applies(sk store.Skill, q Query) bool {
	switch sk.Scope {
	case store.SkillProject:
		if q.ProjectID == "" {
			return false
		}
		return contains(sk.ProjectIDs, q.ProjectID)
	case store.SkillAgentType:
		if q.AgentType == "" {
			return false
		}
		return contains(sk.AgentTypes, q.AgentType)
	default:
		return true
	}
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func reason(sk store.Skill, score float32) string {
	var b strings.Builder
	fmt.Fprintf(&b, "the situation is %.0f%% similar to this skill's trigger", score*100)
	switch sk.Scope {
	case store.SkillProject:
		b.WriteString(", and it is scoped to this project")
	case store.SkillAgentType:
		b.WriteString(", and it is scoped to this agent type")
	}
	if sk.UsageCount > 0 {
		fmt.Fprintf(&b, "; used %d time(s) before", sk.UsageCount)
	}
	return b.String()
}

// Similar finds active skills close enough to a piece of text to be considered
// the same skill. It is what stops the library filling with near-duplicates:
// the answer to "we already have this one" is to update it, not to add another.
func (m *Manager) Similar(ctx context.Context, sk store.Skill, threshold float32) ([]Match, error) {
	matches, err := m.Match(ctx, Query{Text: embedText(sk), TopK: 5, MinScore: threshold})
	if err != nil {
		return nil, err
	}
	out := matches[:0]
	for _, c := range matches {
		if c.Skill.ID != sk.ID {
			out = append(out, c)
		}
	}
	return out, nil
}

// MarkUsed records that a skill was followed.
func (m *Manager) MarkUsed(id string) error { return m.store.TouchSkill(id) }

// --- embedding --------------------------------------------------------------

// Stats describes the library.
type Stats struct {
	Total    int `json:"total"`
	Active   int `json:"active"`
	Draft    int `json:"draft"`
	Disabled int `json:"disabled"`
	Archived int `json:"archived"`
	// Pending counts active skills with no vector for the current model. They
	// exist and are shown, but nothing will match them until they are embedded.
	Pending int    `json:"pending"`
	Model   string `json:"model"`
}

// Stats counts the library by state.
func (m *Manager) Stats() (Stats, error) {
	all, err := m.store.ListSkills(store.SkillFilter{})
	if err != nil {
		return Stats{}, err
	}
	st := Stats{Total: len(all), Model: m.model()}
	for _, sk := range all {
		switch sk.Status {
		case store.SkillActive:
			st.Active++
			if !sk.HasVector || sk.EmbeddingModel != st.Model {
				st.Pending++
			}
		case store.SkillDraft:
			st.Draft++
		case store.SkillDisabled:
			st.Disabled++
		case store.SkillArchived:
			st.Archived++
		}
	}
	return st, nil
}

// EnsureVectors embeds every active skill that has none for the current model.
// This is the recovery path after the runtime was down, or after the embedding
// model changed and every stored vector stopped being comparable.
func (m *Manager) EnsureVectors(ctx context.Context) error {
	model := m.model()
	pending, err := m.store.SkillsToEmbed(model)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	if m.emb == nil {
		return fmt.Errorf("no embedder configured")
	}

	texts := make([]string, len(pending))
	for i, sk := range pending {
		texts[i] = embedText(sk)
	}
	vecs, err := m.emb.Embed(ctx, model, texts)
	if err != nil {
		return err
	}
	if len(vecs) != len(pending) {
		return fmt.Errorf("asked for %d embeddings, got %d", len(pending), len(vecs))
	}
	for i, sk := range pending {
		if err := m.store.SetSkillVector(sk.ID, memory.Normalise(vecs[i]), model); err != nil {
			return err
		}
	}
	return nil
}

// embed vectorises one skill. Failure is not fatal: the skill is stored and
// visible either way, and Stats reports it as pending so the panel can offer to
// try again rather than the skill silently never matching.
func (m *Manager) embed(ctx context.Context, sk store.Skill) error {
	vec, err := m.embedOne(ctx, embedText(sk))
	if err != nil {
		return err
	}
	return m.store.SetSkillVector(sk.ID, vec, m.model())
}

func (m *Manager) embedOne(ctx context.Context, text string) ([]float32, error) {
	if m.emb == nil {
		return nil, fmt.Errorf("no embedder configured")
	}
	vecs, err := m.emb.Embed(ctx, m.model(), []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) != 1 || len(vecs[0]) == 0 {
		return nil, fmt.Errorf("embedder returned nothing for a one-item batch")
	}
	return memory.Normalise(vecs[0]), nil
}
