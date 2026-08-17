package skill

import (
	"fmt"
	"strings"

	"agentmux/internal/orch"
	"agentmux/internal/store"
)

// Limits are the same numbers the JSON Schema hands the model when it drafts a
// skill. They are enforced again here because a schema constrains shape, not
// sense, and because a draft can also arrive from an imported file.
const (
	MaxName        = 60
	MaxDescription = 200
	MaxTrigger     = 400
	MaxSteps       = 12
	MaxStepText    = 300
	MaxConstraint  = 200
	MinTrigger     = 8
)

// Validate normalises a skill in place and reports what is wrong with it.
//
// Normalising and validating are one pass on purpose: the tidying is part of
// what makes the result valid, and splitting them invites saving a skill that
// was checked before it was changed.
func Validate(sk *store.Skill) error {
	sk.Name = strings.TrimSpace(sk.Name)
	sk.Description = strings.TrimSpace(sk.Description)
	sk.Trigger = strings.TrimSpace(sk.Trigger)

	if len(sk.Name) < 2 {
		return fmt.Errorf("a skill needs a name")
	}
	if len([]rune(sk.Name)) > MaxName {
		return fmt.Errorf("the name is longer than %d characters", MaxName)
	}
	if len([]rune(sk.Description)) > MaxDescription {
		return fmt.Errorf("the description is longer than %d characters", MaxDescription)
	}
	if len([]rune(sk.Trigger)) < MinTrigger {
		return fmt.Errorf(
			"the trigger has to say when this applies — it is what a situation is matched against")
	}
	if len([]rune(sk.Trigger)) > MaxTrigger {
		return fmt.Errorf("the trigger is longer than %d characters", MaxTrigger)
	}

	switch sk.Scope {
	case "":
		sk.Scope = store.SkillGlobal
	case store.SkillGlobal, store.SkillProject, store.SkillAgentType:
	default:
		return fmt.Errorf("%q is not a scope", sk.Scope)
	}
	if sk.Scope == store.SkillProject && len(sk.ProjectIDs) == 0 {
		// Without a project this skill can never be matched, and nothing in the
		// UI would explain why it is silently ignored.
		return fmt.Errorf("a project-scoped skill has to name at least one project")
	}
	if sk.Scope == store.SkillAgentType && len(sk.AgentTypes) == 0 {
		return fmt.Errorf("an agent-type-scoped skill has to name at least one agent type")
	}

	steps, err := normaliseSteps(sk.Steps)
	if err != nil {
		return err
	}
	sk.Steps = steps

	constraints := make([]string, 0, len(sk.Constraints))
	for _, c := range sk.Constraints {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if len([]rune(c)) > MaxConstraint {
			return fmt.Errorf("a constraint is longer than %d characters", MaxConstraint)
		}
		constraints = append(constraints, c)
	}
	sk.Constraints = constraints

	if sk.ProjectIDs == nil {
		sk.ProjectIDs = []string{}
	}
	if sk.AgentTypes == nil {
		sk.AgentTypes = []string{}
	}
	if sk.CreatedBy == "" {
		sk.CreatedBy = "user"
	}
	return nil
}

// normaliseSteps trims, drops empties and duplicates, and renumbers.
//
// Renumbering rather than trusting the order field is what keeps a drafted
// skill with three steps all numbered 1 from rendering as an unordered heap.
func normaliseSteps(in []store.SkillStep) ([]store.SkillStep, error) {
	out := make([]store.SkillStep, 0, len(in))
	seen := map[string]bool{}

	for _, s := range in {
		s.Description = strings.TrimSpace(s.Description)
		s.Notes = strings.TrimSpace(s.Notes)
		if s.Description == "" {
			continue
		}
		if len([]rune(s.Description)) > MaxStepText {
			return nil, fmt.Errorf("a step is longer than %d characters", MaxStepText)
		}
		key := strings.ToLower(s.Description)
		if seen[key] {
			continue
		}
		seen[key] = true

		tools := make([]string, 0, len(s.RecommendedTools))
		for _, t := range s.RecommendedTools {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			// A skill that recommends a tool nobody implements would fail at
			// the moment someone followed it. Rejecting it here means the
			// author — human or model — finds out while they are writing.
			if !orch.Known(t) {
				return nil, fmt.Errorf("%q is not a tool AgentMux has", t)
			}
			tools = append(tools, t)
		}
		s.RecommendedTools = tools

		out = append(out, s)
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("a skill needs at least one step")
	}
	if len(out) > MaxSteps {
		return nil, fmt.Errorf("a skill with more than %d steps is a runbook, not a skill", MaxSteps)
	}
	for i := range out {
		out[i].Order = i + 1
	}
	return out, nil
}

// embedText is what a skill is matched on.
//
// The trigger dominates because a situation is being compared against a
// description of that situation. The name and description are included because
// they carry the vocabulary of the domain, which the trigger often assumes.
func embedText(sk store.Skill) string {
	parts := []string{sk.Trigger}
	if sk.Name != "" {
		parts = append(parts, sk.Name)
	}
	if sk.Description != "" {
		parts = append(parts, sk.Description)
	}
	return strings.Join(parts, "\n")
}
