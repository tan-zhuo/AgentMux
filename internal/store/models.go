package store

// AuthType enumerates how AgentMux authenticates to a server.
type AuthType string

const (
	AuthAgent    AuthType = "agent"
	AuthKey      AuthType = "key"
	AuthPassword AuthType = "password"
)

// Folder is a nestable container in the global project tree.
type Folder struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parentId"`
	Sort     int     `json:"sort"`
}

// Project groups workspaces that may live on different servers.
type Project struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	FolderID    *string `json:"folderId"`
	Sort        int     `json:"sort"`
	CreatedAt   int64   `json:"createdAt"`
}

// Server is a reachable SSH host. Secrets never leave the backend: the frontend
// only learns whether a secret is set, via HasPassword / HasPassphrase.
type Server struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	Username      string   `json:"username"`
	AuthType      AuthType `json:"authType"`
	KeyPath       string   `json:"keyPath"`
	HasPassword   bool     `json:"hasPassword"`
	HasPassphrase bool     `json:"hasPassphrase"`
	JumpServerID  *string  `json:"jumpServerId"`
	Tags          []string `json:"tags"`
	Favorite      bool     `json:"favorite"`
	HostKey       string   `json:"hostKey"`
	CreatedAt     int64    `json:"createdAt"`
	LastOKAt      *int64   `json:"lastOkAt"`
}

// ServerInput is the frontend-facing write shape. The secret pointers use the
// tri-state convention: nil leaves the stored secret untouched, "" clears it,
// anything else replaces it.
type ServerInput struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Username     string   `json:"username"`
	AuthType     AuthType `json:"authType"`
	KeyPath      string   `json:"keyPath"`
	Password     *string  `json:"password"`
	Passphrase   *string  `json:"passphrase"`
	JumpServerID *string  `json:"jumpServerId"`
	Tags         []string `json:"tags"`
	Favorite     bool     `json:"favorite"`
}

// Workspace binds a project to an absolute path on one server.
type Workspace struct {
	ID                  string            `json:"id"`
	ProjectID           string            `json:"projectId"`
	ServerID            string            `json:"serverId"`
	Name                string            `json:"name"`
	RemotePath          string            `json:"remotePath"`
	DefaultTmuxSession  string            `json:"defaultTmuxSession"`
	DefaultAgentCommand string            `json:"defaultAgentCommand"`
	Env                 map[string]string `json:"env"`
	Sort                int               `json:"sort"`
}

// AgentStatus mirrors the states in the spec.
type AgentStatus string

const (
	StatusRunning  AgentStatus = "running"
	StatusIdle     AgentStatus = "idle"
	StatusError    AgentStatus = "error"
	StatusDetached AgentStatus = "detached"
	StatusUnknown  AgentStatus = "unknown"
)

// Agent is a long-lived AI agent process pinned to a tmux session.
type Agent struct {
	ID           string      `json:"id"`
	WorkspaceID  string      `json:"workspaceId"`
	Name         string      `json:"name"`
	Command      string      `json:"command"`
	TmuxSession  string      `json:"tmuxSession"`
	TmuxWindow   string      `json:"tmuxWindow"`
	TmuxPaneID   string      `json:"tmuxPaneId"`
	Status       AgentStatus `json:"status"`
	LastSeen     *int64      `json:"lastSeen"`
	PID          *int        `json:"pid"`
	ProgressText string      `json:"progressText"`
	CreatedAt    int64       `json:"createdAt"`
}

// TerminalTab persists an open terminal so the layout survives a restart.
type TerminalTab struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	ServerID    string `json:"serverId"`
	WorkspaceID string `json:"workspaceId"`
	AgentID     string `json:"agentId"`
	TmuxSession string `json:"tmuxSession"`
	Kind        string `json:"kind"` // shell | tmux | agent | command
	Command     string `json:"command"`
	Sort        int    `json:"sort"`
}
