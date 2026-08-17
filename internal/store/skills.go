package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const skillCols = `id, name, description, trigger_text, scope, project_ids, agent_types,
	steps, constraints, examples, version, status, created_by, origin_run_id, confidence,
	embedding_model, dim, embedding IS NOT NULL, created_at, updated_at,
	usage_count, last_used_at, success_count, failure_count`

func scanSkill(sc interface{ Scan(...any) error }) (Skill, error) {
	var (
		s                                          Skill
		projectIDs, agentTypes, steps, constraints string
		examples                                   string
		confidence                                 sql.NullFloat64
		lastUsed                                   sql.NullInt64
	)
	err := sc.Scan(&s.ID, &s.Name, &s.Description, &s.Trigger, &s.Scope,
		&projectIDs, &agentTypes, &steps, &constraints, &examples,
		&s.Version, &s.Status, &s.CreatedBy, &s.OriginRunID, &confidence,
		&s.EmbeddingModel, &s.Dim, &s.HasVector, &s.CreatedAt, &s.UpdatedAt,
		&s.UsageCount, &lastUsed, &s.SuccessCount, &s.FailureCount)
	if err != nil {
		return Skill{}, err
	}

	s.ProjectIDs = decodeStrings(projectIDs)
	s.AgentTypes = decodeStrings(agentTypes)
	s.Constraints = decodeStrings(constraints)
	s.Steps = []SkillStep{}
	_ = json.Unmarshal([]byte(steps), &s.Steps)
	if s.Steps == nil {
		s.Steps = []SkillStep{}
	}
	_ = json.Unmarshal([]byte(examples), &s.Examples)

	if confidence.Valid {
		v := confidence.Float64
		s.Confidence = &v
	}
	if lastUsed.Valid {
		v := lastUsed.Int64
		s.LastUsedAt = &v
	}
	return s, nil
}

func (f SkillFilter) where() (string, []any) {
	var (
		clauses []string
		args    []any
	)
	if len(f.Statuses) > 0 {
		clauses = append(clauses, `status IN (`+strings.TrimSuffix(strings.Repeat("?,", len(f.Statuses)), ",")+`)`)
		for _, s := range f.Statuses {
			args = append(args, s)
		}
	}
	if f.Scope != "" {
		clauses = append(clauses, `scope = ?`)
		args = append(args, f.Scope)
	}
	if f.CreatedBy != "" {
		clauses = append(clauses, `created_by = ?`)
		args = append(args, f.CreatedBy)
	}
	if f.ProjectID != "" {
		// A global skill applies everywhere, so filtering by project has to
		// keep it rather than only matching the ones that name the project.
		clauses = append(clauses, `(scope = 'global' OR project_ids LIKE ? ESCAPE '\')`)
		args = append(args, "%\""+escapeLike(f.ProjectID)+"\"%")
	}
	if t := strings.TrimSpace(f.Text); t != "" {
		clauses = append(clauses,
			`(name LIKE ? ESCAPE '\' OR description LIKE ? ESCAPE '\' OR trigger_text LIKE ? ESCAPE '\')`)
		like := "%" + escapeLike(t) + "%"
		args = append(args, like, like, like)
	}
	if len(clauses) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// ListSkills returns matching skills, most recently updated first.
func (s *Store) ListSkills(f SkillFilter) ([]Skill, error) {
	clause, args := f.where()
	rows, err := s.db.Query(`SELECT `+skillCols+` FROM skills`+clause+` ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Skill{}
	for rows.Next() {
		sk, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// GetSkill loads one skill.
func (s *Store) GetSkill(id string) (Skill, error) {
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE id = ?`, id)
	sk, err := scanSkill(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Skill{}, fmt.Errorf("skill %s: %w", id, ErrNotFound)
	}
	return sk, err
}

// InsertSkill writes a new skill.
func (s *Store) InsertSkill(sk Skill) (Skill, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	now := time.Now().Unix()
	if sk.ID == "" {
		sk.ID = uuid.NewString()
	}
	sk.Version = 1
	sk.CreatedAt, sk.UpdatedAt = now, now

	var blob any
	if len(sk.Embedding) > 0 {
		blob = EncodeVector(sk.Embedding)
		sk.Dim = len(sk.Embedding)
		sk.HasVector = true
	}
	var confidence any
	if sk.Confidence != nil {
		confidence = *sk.Confidence
	}

	_, err := s.db.Exec(`INSERT INTO skills
		(id, name, description, trigger_text, scope, project_ids, agent_types, steps,
		 constraints, examples, version, status, created_by, origin_run_id, confidence,
		 embedding, embedding_model, dim, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sk.ID, sk.Name, sk.Description, sk.Trigger, string(sk.Scope),
		jsonEncode(sk.ProjectIDs), jsonEncode(sk.AgentTypes), jsonEncode(sk.Steps),
		jsonEncode(sk.Constraints), jsonEncode(sk.Examples),
		sk.Version, string(sk.Status), sk.CreatedBy, sk.OriginRunID, confidence,
		blob, sk.EmbeddingModel, sk.Dim, sk.CreatedAt, sk.UpdatedAt)
	return sk, err
}

// UpdateSkill rewrites the content of a skill and bumps its version.
//
// The vector is cleared rather than kept: the trigger may have changed, and a
// stale vector would keep matching the old wording while the panel shows the
// new one. Whoever calls this re-embeds if the skill is active.
func (s *Store) UpdateSkill(sk Skill) (Skill, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	sk.UpdatedAt = time.Now().Unix()
	var confidence any
	if sk.Confidence != nil {
		confidence = *sk.Confidence
	}

	_, err := s.db.Exec(`UPDATE skills SET
			name = ?, description = ?, trigger_text = ?, scope = ?, project_ids = ?,
			agent_types = ?, steps = ?, constraints = ?, examples = ?, version = ?,
			status = ?, confidence = ?, updated_at = ?,
			embedding = NULL, embedding_model = '', dim = 0
		WHERE id = ?`,
		sk.Name, sk.Description, sk.Trigger, string(sk.Scope), jsonEncode(sk.ProjectIDs),
		jsonEncode(sk.AgentTypes), jsonEncode(sk.Steps), jsonEncode(sk.Constraints),
		jsonEncode(sk.Examples), sk.Version, string(sk.Status), confidence, sk.UpdatedAt,
		sk.ID)
	if err != nil {
		return Skill{}, err
	}
	row := s.db.QueryRow(`SELECT `+skillCols+` FROM skills WHERE id = ?`, sk.ID)
	return scanSkill(row)
}

// SetSkillStatus moves a skill to another state without touching its content.
func (s *Store) SetSkillStatus(id string, status SkillStatus) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`UPDATE skills SET status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now().Unix(), id)
	return err
}

// SetSkillVector attaches an embedding.
func (s *Store) SetSkillVector(id string, vec []float32, model string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`UPDATE skills SET embedding = ?, embedding_model = ?, dim = ? WHERE id = ?`,
		EncodeVector(vec), model, len(vec), id)
	return err
}

// ClearSkillVector removes an embedding, which is how a skill leaves the
// matching pool the moment it stops being active.
func (s *Store) ClearSkillVector(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(
		`UPDATE skills SET embedding = NULL, embedding_model = '', dim = 0 WHERE id = ?`, id)
	return err
}

// DeleteSkill removes a skill and its version history.
func (s *Store) DeleteSkill(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`DELETE FROM skills WHERE id = ?`, id)
	return err
}

// TouchSkill records that a skill was used in planning.
func (s *Store) TouchSkill(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(
		`UPDATE skills SET usage_count = usage_count + 1, last_used_at = ? WHERE id = ?`,
		time.Now().Unix(), id)
	return err
}

// ActiveSkillVectors loads the vectors eligible for matching. Only active
// skills have one at all, so no status filter is needed here — but the join
// against the embedding model still is, for the same reason as memories.
func (s *Store) ActiveSkillVectors(model string, dim int) ([]VectorRow, error) {
	rows, err := s.db.Query(
		`SELECT id, embedding FROM skills
		 WHERE embedding IS NOT NULL AND embedding_model = ? AND dim = ? AND status = 'active'`,
		model, dim)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []VectorRow{}
	for rows.Next() {
		var (
			id   string
			blob []byte
		)
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		v, err := DecodeVector(blob)
		if err != nil {
			return nil, fmt.Errorf("skill %s: %w", id, err)
		}
		out = append(out, VectorRow{ID: id, Vec: v})
	}
	return out, rows.Err()
}

// SkillsToEmbed returns active skills with no vector for this model.
func (s *Store) SkillsToEmbed(model string) ([]Skill, error) {
	rows, err := s.db.Query(`SELECT `+skillCols+` FROM skills
		WHERE status = 'active' AND (embedding IS NULL OR embedding_model <> ?)`, model)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Skill{}
	for rows.Next() {
		sk, err := scanSkill(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sk)
	}
	return out, rows.Err()
}

// --- versions ---------------------------------------------------------------

// AddSkillVersion snapshots a skill as it was, before an edit replaces it.
func (s *Store) AddSkillVersion(sk Skill, note, changedBy string) error {
	blob, err := json.Marshal(sk)
	if err != nil {
		return err
	}

	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err = s.db.Exec(`INSERT INTO skill_versions
		(id, skill_id, version, snapshot, note, changed_by, created_at)
		VALUES(?,?,?,?,?,?,?)`,
		uuid.NewString(), sk.ID, sk.Version, string(blob), note, changedBy, time.Now().Unix())
	return err
}

// ListSkillVersions returns the history of one skill, newest first.
func (s *Store) ListSkillVersions(skillID string) ([]SkillVersion, error) {
	rows, err := s.db.Query(`SELECT id, skill_id, version, snapshot, note, changed_by, created_at
		FROM skill_versions WHERE skill_id = ? ORDER BY version DESC`, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SkillVersion{}
	for rows.Next() {
		var (
			v    SkillVersion
			blob string
		)
		if err := rows.Scan(&v.ID, &v.SkillID, &v.Version, &blob, &v.Note, &v.ChangedBy, &v.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(blob), &v.Snapshot); err != nil {
			return nil, fmt.Errorf("version %d of skill %s: %w", v.Version, skillID, err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetSkillVersion loads one historical version.
func (s *Store) GetSkillVersion(skillID string, version int) (SkillVersion, error) {
	row := s.db.QueryRow(`SELECT id, skill_id, version, snapshot, note, changed_by, created_at
		FROM skill_versions WHERE skill_id = ? AND version = ?`, skillID, version)

	var (
		v    SkillVersion
		blob string
	)
	err := row.Scan(&v.ID, &v.SkillID, &v.Version, &blob, &v.Note, &v.ChangedBy, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return SkillVersion{}, fmt.Errorf("skill %s version %d: %w", skillID, version, ErrNotFound)
	}
	if err != nil {
		return SkillVersion{}, err
	}
	err = json.Unmarshal([]byte(blob), &v.Snapshot)
	return v, err
}
