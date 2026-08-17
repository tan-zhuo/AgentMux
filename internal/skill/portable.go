package skill

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"agentmux/internal/store"
)

// Portable is a skill stripped of everything local to one installation.
//
// Ids, vectors, usage counters and timestamps are all left out: they describe
// this machine's copy, not the skill. Carrying them would make an imported
// skill claim a history it never had, and would make two libraries that share a
// skill disagree about how well it works.
type Portable struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Trigger     string              `json:"trigger"`
	Scope       store.SkillScope    `json:"scope"`
	ProjectIDs  []string            `json:"projectIds,omitempty"`
	AgentTypes  []string            `json:"agentTypes,omitempty"`
	Steps       []store.SkillStep   `json:"steps"`
	Constraints []string            `json:"constraints,omitempty"`
	Examples    store.SkillExamples `json:"examples,omitempty"`
	Status      store.SkillStatus   `json:"status"`
	CreatedBy   string              `json:"createdBy"`
	Confidence  *float64            `json:"confidence,omitempty"`
}

// Bundle is the file format: a version marker and some skills.
type Bundle struct {
	Format string     `json:"format"`
	Skills []Portable `json:"skills"`
}

// BundleFormat identifies the file layout, so a future change can be detected
// rather than guessed at.
const BundleFormat = "agentmux.skills/v1"

func toPortable(sk store.Skill) Portable {
	return Portable{
		Name:        sk.Name,
		Description: sk.Description,
		Trigger:     sk.Trigger,
		Scope:       sk.Scope,
		ProjectIDs:  sk.ProjectIDs,
		AgentTypes:  sk.AgentTypes,
		Steps:       sk.Steps,
		Constraints: sk.Constraints,
		Examples:    sk.Examples,
		Status:      sk.Status,
		CreatedBy:   sk.CreatedBy,
		Confidence:  sk.Confidence,
	}
}

// ExportJSON writes the named skills, or the whole library when ids is empty.
func (m *Manager) ExportJSON(ids []string) (string, error) {
	var skills []store.Skill
	if len(ids) == 0 {
		all, err := m.store.ListSkills(store.SkillFilter{})
		if err != nil {
			return "", err
		}
		skills = all
	} else {
		for _, id := range ids {
			sk, err := m.store.GetSkill(id)
			if err != nil {
				return "", err
			}
			skills = append(skills, sk)
		}
	}

	bundle := Bundle{Format: BundleFormat, Skills: make([]Portable, 0, len(skills))}
	for _, sk := range skills {
		bundle.Skills = append(bundle.Skills, toPortable(sk))
	}
	out, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ImportResult reports what an import did, including what it refused.
//
// A partial import is the normal case — one bad skill in a file of twenty
// should not cost the other nineteen — so the refusals are returned rather than
// aborting, and named so they can be fixed.
type ImportResult struct {
	Imported int      `json:"imported"`
	Skipped  []string `json:"skipped"`
}

// ImportJSON adds skills from a bundle.
//
// Status is preserved rather than forced to draft: the file came from a library
// somebody curated, and dropping that distinction would make export and import
// lossy in the one field a reviewer cares about. What is not preserved is any
// claim to authority — every imported skill is validated exactly like one typed
// in by hand, tool names included.
func (m *Manager) ImportJSON(ctx context.Context, data string) (ImportResult, error) {
	var bundle Bundle
	if err := json.Unmarshal([]byte(data), &bundle); err != nil {
		return ImportResult{}, fmt.Errorf("this does not look like a skill bundle: %w", err)
	}
	if bundle.Format != "" && bundle.Format != BundleFormat {
		return ImportResult{}, fmt.Errorf("unknown bundle format %q", bundle.Format)
	}
	if len(bundle.Skills) == 0 {
		return ImportResult{}, fmt.Errorf("the bundle contains no skills")
	}

	res := ImportResult{Skipped: []string{}}
	for _, p := range bundle.Skills {
		sk := store.Skill{
			Name:        p.Name,
			Description: p.Description,
			Trigger:     p.Trigger,
			Scope:       p.Scope,
			ProjectIDs:  p.ProjectIDs,
			AgentTypes:  p.AgentTypes,
			Steps:       p.Steps,
			Constraints: p.Constraints,
			Examples:    p.Examples,
			Status:      p.Status,
			CreatedBy:   p.CreatedBy,
			Confidence:  p.Confidence,
		}
		if sk.Status == "" {
			sk.Status = store.SkillDraft
		}
		// Create() forces orchestrator-authored skills to draft. An import is a
		// human action, so authorship of the file is not authority here: the
		// status in the file stands, but a skill the file says a model wrote
		// still lands as a draft.
		if _, err := m.Create(ctx, sk); err != nil {
			name := p.Name
			if name == "" {
				name = "(unnamed)"
			}
			res.Skipped = append(res.Skipped, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		res.Imported++
	}
	return res, nil
}

// ExportMarkdown renders skills for people: a pull request, a wiki, a review.
//
// It is deliberately one-way. Round-tripping hand-edited Markdown would mean
// parsing whatever someone typed and maintaining a second import path that has
// to stay equivalent to the first. JSON is the interchange format; this is for
// reading.
func (m *Manager) ExportMarkdown(ids []string) (string, error) {
	var skills []store.Skill
	if len(ids) == 0 {
		all, err := m.store.ListSkills(store.SkillFilter{})
		if err != nil {
			return "", err
		}
		skills = all
	} else {
		for _, id := range ids {
			sk, err := m.store.GetSkill(id)
			if err != nil {
				return "", err
			}
			skills = append(skills, sk)
		}
	}

	var b strings.Builder
	b.WriteString("# Skills\n\n")
	b.WriteString("Exported from AgentMux for reading and review. ")
	b.WriteString("Edits made here do not travel back — change skills in the app, ")
	b.WriteString("or exchange them as JSON.\n")

	for _, sk := range skills {
		fmt.Fprintf(&b, "\n## %s\n\n", sk.Name)
		fmt.Fprintf(&b, "- **Status** %s (v%d, by %s)\n", sk.Status, sk.Version, sk.CreatedBy)
		fmt.Fprintf(&b, "- **Scope** %s", sk.Scope)
		if len(sk.ProjectIDs) > 0 {
			fmt.Fprintf(&b, " — projects %s", strings.Join(sk.ProjectIDs, ", "))
		}
		if len(sk.AgentTypes) > 0 {
			fmt.Fprintf(&b, " — agent types %s", strings.Join(sk.AgentTypes, ", "))
		}
		b.WriteString("\n")
		if sk.UsageCount > 0 {
			fmt.Fprintf(&b, "- **Used** %d time(s)\n", sk.UsageCount)
		}
		if sk.Description != "" {
			fmt.Fprintf(&b, "\n%s\n", sk.Description)
		}
		fmt.Fprintf(&b, "\n**When it applies.** %s\n\n### Steps\n\n", sk.Trigger)
		for _, s := range sk.Steps {
			fmt.Fprintf(&b, "%d. %s\n", s.Order, s.Description)
			if len(s.RecommendedTools) > 0 {
				fmt.Fprintf(&b, "   - Tools: `%s`\n", strings.Join(s.RecommendedTools, "`, `"))
			}
			if s.Notes != "" {
				fmt.Fprintf(&b, "   - %s\n", s.Notes)
			}
		}
		if len(sk.Constraints) > 0 {
			b.WriteString("\n### Constraints\n\n")
			for _, c := range sk.Constraints {
				fmt.Fprintf(&b, "- %s\n", c)
			}
		}
		if sk.Examples.Success != "" {
			fmt.Fprintf(&b, "\n### Worked\n\n%s\n", sk.Examples.Success)
		}
		if sk.Examples.Failure != "" {
			fmt.Fprintf(&b, "\n### Did not work\n\n%s\n", sk.Examples.Failure)
		}
	}
	return b.String(), nil
}
