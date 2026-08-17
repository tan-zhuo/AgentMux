package app

import (
	"context"
	"time"

	"agentmux/internal/orch"
	"agentmux/internal/skill"
	"agentmux/internal/store"
)

// SkillService exposes the skill library to the frontend.
type SkillService struct{ core *Core }

// NewSkillService binds a skill service to the core.
func NewSkillService(c *Core) *SkillService { return &SkillService{core: c} }

// ServiceName identifies the service in Wails logs.
func (s *SkillService) ServiceName() string { return "SkillService" }

// embedCtx bounds anything that may have to wait on a local model.
func embedCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Minute)
}

// List returns matching skills.
func (s *SkillService) List(f store.SkillFilter) ([]store.Skill, error) {
	return s.core.Skills.List(f)
}

// Get loads one skill.
func (s *SkillService) Get(id string) (store.Skill, error) { return s.core.Skills.Get(id) }

// Create stores a new skill.
func (s *SkillService) Create(sk store.Skill) (store.Skill, error) {
	ctx, cancel := embedCtx()
	defer cancel()
	return s.core.Skills.Create(ctx, sk)
}

// Update replaces a skill's content, keeping the previous version.
func (s *SkillService) Update(sk store.Skill, note string) (store.Skill, error) {
	ctx, cancel := embedCtx()
	defer cancel()
	return s.core.Skills.Update(ctx, sk, note)
}

// Delete removes a skill and its history.
func (s *SkillService) Delete(id string) error { return s.core.Skills.Delete(id) }

// Apply moves a skill through its lifecycle: approve, reject, disable, enable,
// archive, restore.
func (s *SkillService) Apply(id string, event string) (store.Skill, error) {
	ctx, cancel := embedCtx()
	defer cancel()
	return s.core.Skills.Apply(ctx, id, skill.Event(event))
}

// Versions returns the history of one skill.
func (s *SkillService) Versions(id string) ([]store.SkillVersion, error) {
	return s.core.Skills.Versions(id)
}

// Rollback restores an earlier version as a new one.
func (s *SkillService) Rollback(id string, version int) (store.Skill, error) {
	ctx, cancel := embedCtx()
	defer cancel()
	return s.core.Skills.Rollback(ctx, id, version)
}

// Match answers "which skills apply here?" for a described situation.
//
// This is also the test bench: typing a scenario and seeing what would be
// matched, and why, is the only way to find out that a skill's trigger says
// something other than what its author meant.
func (s *SkillService) Match(q skill.Query) ([]skill.Match, error) {
	ctx, cancel := embedCtx()
	defer cancel()
	return s.core.Skills.Match(ctx, q)
}

// Stats counts the library by state.
func (s *SkillService) Stats() (skill.Stats, error) { return s.core.Skills.Stats() }

// Embed fills in vectors for active skills that have none, which is the
// recovery path after the runtime was down or the embedding model changed.
func (s *SkillService) Embed() error {
	ctx, cancel := embedCtx()
	defer cancel()
	return s.core.Skills.EnsureVectors(ctx)
}

// ExportJSON returns a bundle of skills. An empty id list exports everything.
func (s *SkillService) ExportJSON(ids []string) (string, error) {
	return s.core.Skills.ExportJSON(ids)
}

// ExportMarkdown renders skills for reading. It is one-way by design.
func (s *SkillService) ExportMarkdown(ids []string) (string, error) {
	return s.core.Skills.ExportMarkdown(ids)
}

// ImportJSON adds skills from a bundle, validating each one exactly as if it
// had been typed in here.
func (s *SkillService) ImportJSON(data string) (skill.ImportResult, error) {
	ctx, cancel := embedCtx()
	defer cancel()
	return s.core.Skills.ImportJSON(ctx, data)
}

// Tools lists the tools a skill may recommend, so the editor can offer them
// instead of letting someone invent a name that will be rejected on save.
func (s *SkillService) Tools() []orch.ToolMeta { return orch.Catalog() }
