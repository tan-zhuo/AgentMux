package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const agentCols = `id, workspace_id, name, command, tmux_session, tmux_window, tmux_pane_id,
	status, activity, attention, last_seen, pid, progress_text, created_at`

func scanAgent(sc interface{ Scan(...any) error }) (Agent, error) {
	var (
		a        Agent
		lastSeen sql.NullInt64
		pid      sql.NullInt64
	)
	err := sc.Scan(&a.ID, &a.WorkspaceID, &a.Name, &a.Command, &a.TmuxSession, &a.TmuxWindow,
		&a.TmuxPaneID, &a.Status, &a.Activity, &a.Attention, &lastSeen, &pid, &a.ProgressText, &a.CreatedAt)
	if err != nil {
		return Agent{}, err
	}
	if lastSeen.Valid {
		v := lastSeen.Int64
		a.LastSeen = &v
	}
	if pid.Valid {
		v := int(pid.Int64)
		a.PID = &v
	}
	return a, nil
}

// ListAgents returns every configured agent.
func (s *Store) ListAgents() ([]Agent, error) {
	rows, err := s.db.Query(`SELECT ` + agentCols + ` FROM agents ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetAgent loads one agent by id.
func (s *Store) GetAgent(id string) (Agent, error) {
	row := s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE id = ?`, id)
	a, err := scanAgent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Agent{}, fmt.Errorf("agent %s: %w", id, ErrNotFound)
	}
	return a, err
}

// SaveAgent inserts or updates an agent definition.
func (s *Store) SaveAgent(a Agent) (Agent, error) {
	if strings.TrimSpace(a.Name) == "" {
		return Agent{}, errors.New("agent name is required")
	}
	if a.WorkspaceID == "" {
		return Agent{}, errors.New("agent needs a workspace")
	}
	if strings.TrimSpace(a.Command) == "" {
		return Agent{}, errors.New("agent command is required")
	}
	if a.Status == "" {
		a.Status = StatusUnknown
	}

	s.wmu.Lock()
	defer s.wmu.Unlock()

	if a.ID == "" {
		a.ID = uuid.NewString()
		a.CreatedAt = time.Now().Unix()
		_, err := s.db.Exec(`INSERT INTO agents
			(id, workspace_id, name, command, tmux_session, tmux_window, tmux_pane_id,
			 status, progress_text, created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			a.ID, a.WorkspaceID, a.Name, a.Command, a.TmuxSession, a.TmuxWindow, a.TmuxPaneID,
			string(a.Status), a.ProgressText, a.CreatedAt)
		return a, err
	}
	_, err := s.db.Exec(`UPDATE agents SET
			workspace_id = ?, name = ?, command = ?, tmux_session = ?, tmux_window = ?
		WHERE id = ?`,
		a.WorkspaceID, a.Name, a.Command, a.TmuxSession, a.TmuxWindow, a.ID)
	if err != nil {
		return Agent{}, err
	}
	return s.getAgentLocked(a.ID)
}

func (s *Store) getAgentLocked(id string) (Agent, error) {
	row := s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE id = ?`, id)
	return scanAgent(row)
}

// UpdateAgentRuntime persists the polled runtime state of an agent. Attention
// is deliberately not part of it: runtime state is overwritten on every poll,
// and a mark meant to outlive the moment it was raised must not be.
func (s *Store) UpdateAgentRuntime(id string, status AgentStatus, activity AgentActivity, pid *int, progress string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	var pidVal any
	if pid != nil {
		pidVal = *pid
	}
	_, err := s.db.Exec(`UPDATE agents SET status = ?, activity = ?, pid = ?, progress_text = ?, last_seen = ? WHERE id = ?`,
		string(status), string(activity), pidVal, progress, time.Now().Unix(), id)
	return err
}

// SetAgentAttention raises or clears the sticky "needs a human" mark.
func (s *Store) SetAgentAttention(id string, attention AgentAttention) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`UPDATE agents SET attention = ? WHERE id = ?`, string(attention), id)
	return err
}

// SetAgentPane records the stable tmux pane id an agent was launched into, so
// later status checks and messages address the same pane even after the user
// adds windows to the session.
func (s *Store) SetAgentPane(id, session, paneID string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`UPDATE agents SET tmux_session = ?, tmux_pane_id = ? WHERE id = ?`,
		session, paneID, id)
	return err
}

// DeleteAgent removes an agent definition. The remote tmux session is untouched.
func (s *Store) DeleteAgent(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, id)
	return err
}

// --- terminal tabs ----------------------------------------------------------

// ListTabs returns the persisted terminal layout.
func (s *Store) ListTabs() ([]TerminalTab, error) {
	rows, err := s.db.Query(`SELECT id, title, server_id, workspace_id, agent_id, tmux_session, kind, command, sort
		FROM terminal_tabs ORDER BY sort`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TerminalTab{}
	for rows.Next() {
		var t TerminalTab
		if err := rows.Scan(&t.ID, &t.Title, &t.ServerID, &t.WorkspaceID, &t.AgentID,
			&t.TmuxSession, &t.Kind, &t.Command, &t.Sort); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ReplaceTabs rewrites the persisted terminal layout in one shot.
func (s *Store) ReplaceTabs(tabs []TerminalTab) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM terminal_tabs`); err != nil {
		return err
	}
	for i, t := range tabs {
		if _, err := tx.Exec(`INSERT INTO terminal_tabs
			(id, title, server_id, workspace_id, agent_id, tmux_session, kind, command, sort)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			t.ID, t.Title, t.ServerID, t.WorkspaceID, t.AgentID, t.TmuxSession, t.Kind, t.Command, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}
