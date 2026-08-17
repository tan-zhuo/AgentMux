package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// --- runs -------------------------------------------------------------------

const runCols = `id, goal, trigger, COALESCE(project_id, ''), status, model, skill_ids,
	started_at, ended_at, summary, error`

func scanRun(sc interface{ Scan(...any) error }) (Run, error) {
	var (
		r        Run
		skillIDs string
		ended    sql.NullInt64
	)
	err := sc.Scan(&r.ID, &r.Goal, &r.Trigger, &r.ProjectID, &r.Status, &r.Model,
		&skillIDs, &r.StartedAt, &ended, &r.Summary, &r.Error)
	if err != nil {
		return Run{}, err
	}
	r.SkillIDs = decodeStrings(skillIDs)
	if ended.Valid {
		v := ended.Int64
		r.EndedAt = &v
	}
	return r, nil
}

// CreateRun opens a run.
func (s *Store) CreateRun(r Run) (Run, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	r.ID = uuid.NewString()
	r.StartedAt = time.Now().Unix()
	if r.Status == "" {
		r.Status = RunRunning
	}
	_, err := s.db.Exec(`INSERT INTO orch_runs
		(id, goal, trigger, project_id, status, model, skill_ids, started_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.Goal, r.Trigger, nullableString(&r.ProjectID), string(r.Status), r.Model,
		jsonEncode(r.SkillIDs), r.StartedAt)
	return r, err
}

// UpdateRunStatus records where a run got to.
func (s *Store) UpdateRunStatus(id string, status RunStatus, summary, errText string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	var ended any
	switch status {
	case RunSucceeded, RunFailed, RunCancelled:
		ended = time.Now().Unix()
	}
	_, err := s.db.Exec(
		`UPDATE orch_runs SET status = ?, summary = ?, error = ?, ended_at = ? WHERE id = ?`,
		string(status), summary, errText, ended, id)
	return err
}

// SetRunSkills records which skills the plan was built on.
func (s *Store) SetRunSkills(id string, skillIDs []string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`UPDATE orch_runs SET skill_ids = ? WHERE id = ?`, jsonEncode(skillIDs), id)
	return err
}

// GetRun loads one run.
func (s *Store) GetRun(id string) (Run, error) {
	row := s.db.QueryRow(`SELECT `+runCols+` FROM orch_runs WHERE id = ?`, id)
	r, err := scanRun(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, fmt.Errorf("run %s: %w", id, ErrNotFound)
	}
	return r, err
}

// ListRuns returns recent runs, newest first.
func (s *Store) ListRuns(limit int) ([]Run, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(
		fmt.Sprintf(`SELECT %s FROM orch_runs ORDER BY started_at DESC LIMIT %d`, runCols, limit))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Run{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecoverRunningRuns marks runs that were live when the app stopped.
//
// A run only exists inside a running process, so anything still marked running
// at startup died with the last one. Leaving them as they are would show an
// orchestrator eternally working on something nobody is doing.
func (s *Store) RecoverRunningRuns() error {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	if _, err := s.db.Exec(
		`UPDATE orch_runs SET status = ?, error = ?, ended_at = ?
		 WHERE status IN (?, ?)`,
		string(RunCancelled), "AgentMux stopped while this run was in progress",
		time.Now().Unix(), string(RunRunning), string(RunWaiting)); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`UPDATE approvals SET status = ?, decided_at = ? WHERE status = ?`,
		string(ApprovalExpired), time.Now().Unix(), string(ApprovalPending))
	return err
}

// --- steps ------------------------------------------------------------------

const stepCols = `id, run_id, seq, phase, tool, args, result, reasoning, skill_id,
	memory_ids, injection_flag, risk, outcome, duration_ms, created_at`

// AddStep appends one step to a run's log.
func (s *Store) AddStep(st Step) (Step, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	st.ID = uuid.NewString()
	st.CreatedAt = time.Now().Unix()
	_, err := s.db.Exec(`INSERT INTO orch_steps
		(id, run_id, seq, phase, tool, args, result, reasoning, skill_id, memory_ids,
		 injection_flag, risk, outcome, duration_ms, created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		st.ID, st.RunID, st.Seq, st.Phase, st.Tool, st.Args, st.Result, st.Reasoning,
		st.SkillID, jsonEncode(st.MemoryIDs), st.InjectionFlag, st.Risk, st.Outcome,
		st.DurationMs, st.CreatedAt)
	return st, err
}

// ListSteps returns the log of one run in order.
func (s *Store) ListSteps(runID string) ([]Step, error) {
	rows, err := s.db.Query(`SELECT `+stepCols+` FROM orch_steps WHERE run_id = ? ORDER BY seq`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Step{}
	for rows.Next() {
		var (
			st        Step
			memoryIDs string
		)
		if err := rows.Scan(&st.ID, &st.RunID, &st.Seq, &st.Phase, &st.Tool, &st.Args,
			&st.Result, &st.Reasoning, &st.SkillID, &memoryIDs, &st.InjectionFlag,
			&st.Risk, &st.Outcome, &st.DurationMs, &st.CreatedAt); err != nil {
			return nil, err
		}
		st.MemoryIDs = decodeStrings(memoryIDs)
		out = append(out, st)
	}
	return out, rows.Err()
}

// --- approvals --------------------------------------------------------------

const approvalCols = `id, run_id, tool, args, risk, rationale, target, skill_id,
	injection_flag, status, decided_at, note, created_at, expires_at`

func scanApproval(sc interface{ Scan(...any) error }) (Approval, error) {
	var (
		a       Approval
		decided sql.NullInt64
	)
	err := sc.Scan(&a.ID, &a.RunID, &a.Tool, &a.Args, &a.Risk, &a.Rationale, &a.Target,
		&a.SkillID, &a.InjectionFlag, &a.Status, &decided, &a.Note, &a.CreatedAt, &a.ExpiresAt)
	if err != nil {
		return Approval{}, err
	}
	if decided.Valid {
		v := decided.Int64
		a.DecidedAt = &v
	}
	return a, nil
}

// CreateApproval records a tool call waiting for a person.
func (s *Store) CreateApproval(a Approval) (Approval, error) {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	a.ID = uuid.NewString()
	a.CreatedAt = time.Now().Unix()
	a.Status = ApprovalPending
	_, err := s.db.Exec(`INSERT INTO approvals
		(id, run_id, tool, args, risk, rationale, target, skill_id, injection_flag,
		 status, note, created_at, expires_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,'',?,?)`,
		a.ID, a.RunID, a.Tool, a.Args, a.Risk, a.Rationale, a.Target, a.SkillID,
		a.InjectionFlag, string(a.Status), a.CreatedAt, a.ExpiresAt)
	return a, err
}

// DecideApproval records a decision. It refuses to decide twice, so a late
// click on a request that already timed out cannot resurrect it.
func (s *Store) DecideApproval(id string, status ApprovalStatus, note string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	res, err := s.db.Exec(
		`UPDATE approvals SET status = ?, decided_at = ?, note = ? WHERE id = ? AND status = ?`,
		string(status), time.Now().Unix(), note, id, string(ApprovalPending))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("this request has already been decided or has expired")
	}
	return nil
}

// PendingApprovals lists what is waiting.
func (s *Store) PendingApprovals() ([]Approval, error) {
	rows, err := s.db.Query(`SELECT ` + approvalCols +
		` FROM approvals WHERE status = 'pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Approval{}
	for rows.Next() {
		a, err := scanApproval(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetApproval loads one request.
func (s *Store) GetApproval(id string) (Approval, error) {
	row := s.db.QueryRow(`SELECT `+approvalCols+` FROM approvals WHERE id = ?`, id)
	a, err := scanApproval(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Approval{}, fmt.Errorf("approval %s: %w", id, ErrNotFound)
	}
	return a, err
}
