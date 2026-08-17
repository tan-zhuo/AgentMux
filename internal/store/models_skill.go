package store

// SkillStatus is where a skill sits in its lifecycle.
//
// The transitions are enforced in the skill package, not here; this type only
// names the states. Only StatusActive skills are matched during planning.
type SkillStatus string

const (
	SkillDraft    SkillStatus = "draft"
	SkillActive   SkillStatus = "active"
	SkillDisabled SkillStatus = "disabled"
	SkillArchived SkillStatus = "archived"
	SkillRejected SkillStatus = "rejected"
)

// SkillScope limits which situations a skill is offered in.
type SkillScope string

const (
	SkillGlobal    SkillScope = "global"
	SkillProject   SkillScope = "project"
	SkillAgentType SkillScope = "agent_type"
)

// SkillStep is one step of a reusable procedure.
//
// RecommendedTools names tools rather than carrying anything executable. A
// skill influences how the orchestrator plans; it never becomes a path to
// running something, which is what keeps it from being a way around the tool
// permissions.
type SkillStep struct {
	Order            int      `json:"order"`
	Description      string   `json:"description"`
	RecommendedTools []string `json:"recommendedTools"`
	Notes            string   `json:"notes"`
}

// SkillExamples are optional illustrations for the model and for the reader.
type SkillExamples struct {
	Success string `json:"success"`
	Failure string `json:"failure"`
}

// Skill is a named, reusable decision procedure.
type Skill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// Trigger says when this skill applies, in plain words. It is also what
	// gets embedded: matching a situation against a description of that
	// situation is a closer comparison than matching it against a title.
	Trigger     string        `json:"trigger"`
	Scope       SkillScope    `json:"scope"`
	ProjectIDs  []string      `json:"projectIds"`
	AgentTypes  []string      `json:"agentTypes"`
	Steps       []SkillStep   `json:"steps"`
	Constraints []string      `json:"constraints"`
	Examples    SkillExamples `json:"examples"`

	Version     int         `json:"version"`
	Status      SkillStatus `json:"status"`
	CreatedBy   string      `json:"createdBy"`
	OriginRunID string      `json:"originRunId"`
	Confidence  *float64    `json:"confidence"`

	EmbeddingModel string `json:"embeddingModel"`
	Dim            int    `json:"dim"`
	HasVector      bool   `json:"hasVector"`

	CreatedAt    int64  `json:"createdAt"`
	UpdatedAt    int64  `json:"updatedAt"`
	UsageCount   int    `json:"usageCount"`
	LastUsedAt   *int64 `json:"lastUsedAt"`
	SuccessCount int    `json:"successCount"`
	FailureCount int    `json:"failureCount"`

	Embedding []float32 `json:"-"`
}

// SkillVersion is a snapshot of a skill as it was before an edit.
type SkillVersion struct {
	ID        string `json:"id"`
	SkillID   string `json:"skillId"`
	Version   int    `json:"version"`
	Snapshot  Skill  `json:"snapshot"`
	Note      string `json:"note"`
	ChangedBy string `json:"changedBy"`
	CreatedAt int64  `json:"createdAt"`
}

// SkillFilter narrows a listing.
type SkillFilter struct {
	Statuses  []string `json:"statuses"`
	Scope     string   `json:"scope"`
	ProjectID string   `json:"projectId"`
	Text      string   `json:"text"`
	CreatedBy string   `json:"createdBy"`
}
