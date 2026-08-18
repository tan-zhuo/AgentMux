package portable

import (
	"agentmux/internal/skill"
	"agentmux/internal/store"
)

// Bundle is the plaintext inside the file.
//
// Ids are carried, but only so the references between rows survive the trip:
// a workspace has to be able to say which host it belongs to. Nothing keeps its
// id on arrival — the importing machine mints its own, or points the reference
// at the row it already had.
//
// What is left out is everything that describes this installation rather than
// the configuration: agent status, pids, last-seen times, terminal layout, and
// the vectors that only mean something to the model that produced them.
type Bundle struct {
	Format     string `json:"format"`
	ExportedAt int64  `json:"exportedAt"`
	// HasSecrets says whether the passwords and key passphrases came along, so
	// an import can tell someone that a host will still ask for its password.
	HasSecrets bool `json:"hasSecrets"`

	Folders    []Folder          `json:"folders,omitempty"`
	Projects   []Project         `json:"projects,omitempty"`
	Hosts      []Host            `json:"hosts,omitempty"`
	Workspaces []Workspace       `json:"workspaces,omitempty"`
	Agents     []Agent           `json:"agents,omitempty"`
	Skills     []skill.Portable  `json:"skills,omitempty"`
	Settings   map[string]string `json:"settings,omitempty"`
}

// Folder is a node of the project tree.
type Folder struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	ParentID *string `json:"parentId"`
	Sort     int     `json:"sort"`
}

// Project is a project without its local timestamps.
type Project struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	FolderID    *string `json:"folderId"`
	Sort        int     `json:"sort"`
}

// Host is a server row, secrets included when the export was asked for them.
//
// The pinned host key travels: it belongs to the machine at the other end of
// the connection, not to the computer that first saw it, and carrying it means
// the new install is not asked to trust a fresh key it has no way to check.
type Host struct {
	ID           string           `json:"id"`
	Kind         store.ServerKind `json:"kind"`
	Name         string           `json:"name"`
	Host         string           `json:"host"`
	Port         int              `json:"port"`
	Username     string           `json:"username"`
	AuthType     store.AuthType   `json:"authType"`
	KeyPath      string           `json:"keyPath"`
	Password     string           `json:"password,omitempty"`
	Passphrase   string           `json:"passphrase,omitempty"`
	JumpServerID *string          `json:"jumpServerId"`
	Tags         []string         `json:"tags,omitempty"`
	Favorite     bool             `json:"favorite"`
	HostKey      string           `json:"hostKey,omitempty"`
	TrustLevel   store.TrustLevel `json:"trustLevel"`
}

// Workspace binds a project to a path on a host.
type Workspace struct {
	ID                  string            `json:"id"`
	ProjectID           string            `json:"projectId"`
	ServerID            string            `json:"serverId"`
	Name                string            `json:"name"`
	RemotePath          string            `json:"remotePath"`
	DefaultTmuxSession  string            `json:"defaultTmuxSession"`
	DefaultAgentCommand string            `json:"defaultAgentCommand"`
	Env                 map[string]string `json:"env,omitempty"`
	Sort                int               `json:"sort"`
}

// Agent is an agent definition: what to run, and in which session.
type Agent struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	Name        string `json:"name"`
	Command     string `json:"command"`
	TmuxSession string `json:"tmuxSession"`
}

// Options decides how much of this installation travels.
type Options struct {
	// IncludeSecrets carries SSH passwords and key passphrases, so the hosts
	// connect on the other machine without being asked again. Off, the rows
	// arrive complete but silent, and each one asks for its password once.
	IncludeSecrets bool `json:"includeSecrets"`
	// IncludeLibrary carries the skill library and the settings — the model
	// addresses, the theme, the language and the time zone.
	IncludeLibrary bool `json:"includeLibrary"`
}

// Manifest is what a file contains, in counts. It is what the export reports
// and what the import shows before it changes anything.
type Manifest struct {
	Format     string `json:"format"`
	ExportedAt int64  `json:"exportedAt"`
	HasSecrets bool   `json:"hasSecrets"`
	Hosts      int    `json:"hosts"`
	Folders    int    `json:"folders"`
	Projects   int    `json:"projects"`
	Workspaces int    `json:"workspaces"`
	Agents     int    `json:"agents"`
	Skills     int    `json:"skills"`
	Settings   int    `json:"settings"`
}

// Manifest counts what the bundle holds.
func (b Bundle) Manifest() Manifest {
	return Manifest{
		Format:     b.Format,
		ExportedAt: b.ExportedAt,
		HasSecrets: b.HasSecrets,
		Hosts:      len(b.Hosts),
		Folders:    len(b.Folders),
		Projects:   len(b.Projects),
		Workspaces: len(b.Workspaces),
		Agents:     len(b.Agents),
		Skills:     len(b.Skills),
		Settings:   len(b.Settings),
	}
}

// Tally is how one kind of row fared on the way in.
type Tally struct {
	Added   int `json:"added"`
	Skipped int `json:"skipped"`
}

// Result is what an import did.
//
// Skipped is not a failure: an import never overwrites what is already here, so
// a second run of the same file adds nothing and says so. Notes carry the
// things that are worth knowing and cannot be counted — a key file that is not
// on this machine, a host that arrived without its password.
type Result struct {
	Hosts      Tally    `json:"hosts"`
	Folders    Tally    `json:"folders"`
	Projects   Tally    `json:"projects"`
	Workspaces Tally    `json:"workspaces"`
	Agents     Tally    `json:"agents"`
	Skills     Tally    `json:"skills"`
	Settings   Tally    `json:"settings"`
	Notes      []string `json:"notes"`
}
