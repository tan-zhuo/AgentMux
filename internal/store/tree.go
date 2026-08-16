package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// --- folders ----------------------------------------------------------------

// ListFolders returns every folder in the tree.
func (s *Store) ListFolders() ([]Folder, error) {
	rows, err := s.db.Query(`SELECT id, name, parent_id, sort FROM folders ORDER BY sort, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Folder{}
	for rows.Next() {
		var f Folder
		var parent sql.NullString
		if err := rows.Scan(&f.ID, &f.Name, &parent, &f.Sort); err != nil {
			return nil, err
		}
		if parent.Valid {
			v := parent.String
			f.ParentID = &v
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SaveFolder inserts or updates a folder, rejecting cycles.
func (s *Store) SaveFolder(f Folder) (Folder, error) {
	if strings.TrimSpace(f.Name) == "" {
		return Folder{}, errors.New("folder name is required")
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()

	if f.ID == "" {
		f.ID = uuid.NewString()
		_, err := s.db.Exec(`INSERT INTO folders(id, name, parent_id, sort) VALUES(?,?,?,?)`,
			f.ID, f.Name, nullableString(f.ParentID), f.Sort)
		return f, err
	}
	if f.ParentID != nil {
		if err := s.assertNoFolderCycle(f.ID, *f.ParentID); err != nil {
			return Folder{}, err
		}
	}
	_, err := s.db.Exec(`UPDATE folders SET name = ?, parent_id = ?, sort = ? WHERE id = ?`,
		f.Name, nullableString(f.ParentID), f.Sort, f.ID)
	return f, err
}

func (s *Store) assertNoFolderCycle(id, parentID string) error {
	seen := map[string]bool{id: true}
	cur := parentID
	for cur != "" {
		if seen[cur] {
			return errors.New("that move would create a folder cycle")
		}
		seen[cur] = true
		var next sql.NullString
		err := s.db.QueryRow(`SELECT parent_id FROM folders WHERE id = ?`, cur).Scan(&next)
		if err != nil || !next.Valid {
			return nil
		}
		cur = next.String
	}
	return nil
}

// DeleteFolder removes a folder; children cascade, projects are orphaned to root.
func (s *Store) DeleteFolder(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`DELETE FROM folders WHERE id = ?`, id)
	return err
}

// --- projects ---------------------------------------------------------------

// ListProjects returns every project.
func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT id, name, description, folder_id, sort, created_at
		FROM projects ORDER BY sort, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Project{}
	for rows.Next() {
		var p Project
		var folder sql.NullString
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &folder, &p.Sort, &p.CreatedAt); err != nil {
			return nil, err
		}
		if folder.Valid {
			v := folder.String
			p.FolderID = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveProject inserts or updates a project.
func (s *Store) SaveProject(p Project) (Project, error) {
	if strings.TrimSpace(p.Name) == "" {
		return Project{}, errors.New("project name is required")
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()

	if p.ID == "" {
		p.ID = uuid.NewString()
		p.CreatedAt = time.Now().Unix()
		_, err := s.db.Exec(`INSERT INTO projects(id, name, description, folder_id, sort, created_at)
			VALUES(?,?,?,?,?,?)`, p.ID, p.Name, p.Description, nullableString(p.FolderID), p.Sort, p.CreatedAt)
		return p, err
	}
	_, err := s.db.Exec(`UPDATE projects SET name = ?, description = ?, folder_id = ?, sort = ? WHERE id = ?`,
		p.Name, p.Description, nullableString(p.FolderID), p.Sort, p.ID)
	return p, err
}

// DeleteProject removes a project along with its workspaces and agents.
func (s *Store) DeleteProject(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`DELETE FROM projects WHERE id = ?`, id)
	return err
}

// --- workspaces -------------------------------------------------------------

const workspaceCols = `id, project_id, server_id, name, remote_path,
	default_tmux_session, default_agent_command, env, sort`

func scanWorkspace(sc interface{ Scan(...any) error }) (Workspace, error) {
	var w Workspace
	var envRaw string
	err := sc.Scan(&w.ID, &w.ProjectID, &w.ServerID, &w.Name, &w.RemotePath,
		&w.DefaultTmuxSession, &w.DefaultAgentCommand, &envRaw, &w.Sort)
	if err != nil {
		return Workspace{}, err
	}
	w.Env = decodeMap(envRaw)
	return w, nil
}

// ListWorkspaces returns every workspace across all projects.
func (s *Store) ListWorkspaces() ([]Workspace, error) {
	rows, err := s.db.Query(`SELECT ` + workspaceCols + ` FROM workspaces ORDER BY sort, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Workspace{}
	for rows.Next() {
		w, err := scanWorkspace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// GetWorkspace loads one workspace by id.
func (s *Store) GetWorkspace(id string) (Workspace, error) {
	row := s.db.QueryRow(`SELECT `+workspaceCols+` FROM workspaces WHERE id = ?`, id)
	w, err := scanWorkspace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Workspace{}, fmt.Errorf("workspace %s: %w", id, ErrNotFound)
	}
	return w, err
}

// SaveWorkspace inserts or updates a workspace.
func (s *Store) SaveWorkspace(w Workspace) (Workspace, error) {
	if strings.TrimSpace(w.Name) == "" {
		return Workspace{}, errors.New("workspace name is required")
	}
	if strings.TrimSpace(w.RemotePath) == "" {
		return Workspace{}, errors.New("remote path is required")
	}
	if w.ProjectID == "" || w.ServerID == "" {
		return Workspace{}, errors.New("workspace needs both a project and a server")
	}
	if w.Env == nil {
		w.Env = map[string]string{}
	}

	s.wmu.Lock()
	defer s.wmu.Unlock()

	if w.ID == "" {
		w.ID = uuid.NewString()
		_, err := s.db.Exec(`INSERT INTO workspaces
			(id, project_id, server_id, name, remote_path, default_tmux_session, default_agent_command, env, sort)
			VALUES(?,?,?,?,?,?,?,?,?)`,
			w.ID, w.ProjectID, w.ServerID, w.Name, w.RemotePath,
			w.DefaultTmuxSession, w.DefaultAgentCommand, jsonEncode(w.Env), w.Sort)
		return w, err
	}
	_, err := s.db.Exec(`UPDATE workspaces SET
			project_id = ?, server_id = ?, name = ?, remote_path = ?,
			default_tmux_session = ?, default_agent_command = ?, env = ?, sort = ?
		WHERE id = ?`,
		w.ProjectID, w.ServerID, w.Name, w.RemotePath,
		w.DefaultTmuxSession, w.DefaultAgentCommand, jsonEncode(w.Env), w.Sort, w.ID)
	return w, err
}

// DeleteWorkspace removes a workspace and its agents.
func (s *Store) DeleteWorkspace(id string) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.db.Exec(`DELETE FROM workspaces WHERE id = ?`, id)
	return err
}
