package app

import "agentmux/internal/store"

// TreeService owns the folder / project / workspace hierarchy.
type TreeService struct{ core *Core }

// NewTreeService binds a tree service to the core.
func NewTreeService(c *Core) *TreeService { return &TreeService{core: c} }

// ServiceName identifies the service in Wails logs.
func (t *TreeService) ServiceName() string { return "TreeService" }

// Snapshot is the whole tree in one round trip. With hundreds of projects the
// frontend renders from this single payload instead of chatty per-node calls.
type Snapshot struct {
	Folders    []store.Folder    `json:"folders"`
	Projects   []store.Project   `json:"projects"`
	Servers    []store.Server    `json:"servers"`
	Workspaces []store.Workspace `json:"workspaces"`
	Agents     []store.Agent     `json:"agents"`
}

// Snapshot returns folders, projects, servers, workspaces and agents together.
func (t *TreeService) Snapshot() (Snapshot, error) {
	var (
		s   Snapshot
		err error
	)
	if s.Folders, err = t.core.Store.ListFolders(); err != nil {
		return s, err
	}
	if s.Projects, err = t.core.Store.ListProjects(); err != nil {
		return s, err
	}
	if s.Servers, err = t.core.Store.ListServers(); err != nil {
		return s, err
	}
	if s.Workspaces, err = t.core.Store.ListWorkspaces(); err != nil {
		return s, err
	}
	if s.Agents, err = t.core.Store.ListAgents(); err != nil {
		return s, err
	}
	return s, nil
}

// SaveFolder creates or updates a folder.
func (t *TreeService) SaveFolder(f store.Folder) (store.Folder, error) {
	return t.core.Store.SaveFolder(f)
}

// DeleteFolder removes a folder and its descendants.
func (t *TreeService) DeleteFolder(id string) error { return t.core.Store.DeleteFolder(id) }

// SaveProject creates or updates a project.
func (t *TreeService) SaveProject(p store.Project) (store.Project, error) {
	return t.core.Store.SaveProject(p)
}

// DeleteProject removes a project, its workspaces and its agent definitions.
// Remote tmux sessions are deliberately left running.
func (t *TreeService) DeleteProject(id string) error { return t.core.Store.DeleteProject(id) }

// SaveWorkspace creates or updates a workspace.
func (t *TreeService) SaveWorkspace(w store.Workspace) (store.Workspace, error) {
	return t.core.Store.SaveWorkspace(w)
}

// DeleteWorkspace removes a workspace and its agent definitions.
func (t *TreeService) DeleteWorkspace(id string) error { return t.core.Store.DeleteWorkspace(id) }

// GetSetting reads a UI preference.
func (t *TreeService) GetSetting(key, def string) string { return t.core.Store.GetSetting(key, def) }

// SetSetting writes a UI preference.
func (t *TreeService) SetSetting(key, value string) error { return t.core.Store.SetSetting(key, value) }
