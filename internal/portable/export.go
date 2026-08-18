package portable

import (
	"fmt"
	"time"

	"agentmux/internal/skill"
	"agentmux/internal/store"
)

// TravellingSettings are the preferences that mean the same thing on another
// machine: where the local model runtime lives, and how the window should look
// and read.
//
// The orchestrator's own settings are deliberately not here. Whether a thing
// that acts on its own is switched on is a decision made per machine, and a
// file arriving from somewhere else is not that decision.
var TravellingSettings = []string{
	"theme",
	"language",
	"timezone",
	"llm.baseUrl",
	"llm.chatModel",
	"llm.embedModel",
}

// Build reads this installation into a bundle.
//
// Local hosts travel with everything else. A row for "this computer" describes
// the machine it is read on rather than a machine somewhere, so the copy that
// lands on the other side means that machine instead — which is what makes the
// projects hanging off it arrive whole. The import folds it into the local host
// already there rather than standing up a second one.
func Build(st *store.Store, opt Options) (Bundle, error) {
	b := Bundle{
		Format:     FileFormat,
		ExportedAt: time.Now().Unix(),
		HasSecrets: opt.IncludeSecrets,
	}

	folders, err := st.ListFolders()
	if err != nil {
		return Bundle{}, err
	}
	for _, f := range folders {
		b.Folders = append(b.Folders, Folder{ID: f.ID, Name: f.Name, ParentID: f.ParentID, Sort: f.Sort})
	}

	projects, err := st.ListProjects()
	if err != nil {
		return Bundle{}, err
	}
	for _, p := range projects {
		b.Projects = append(b.Projects, Project{
			ID:          p.ID,
			Name:        p.Name,
			Description: p.Description,
			FolderID:    p.FolderID,
			Sort:        p.Sort,
		})
	}

	servers, err := st.ListServers()
	if err != nil {
		return Bundle{}, err
	}
	for _, s := range servers {
		h := Host{
			ID:           s.ID,
			Kind:         s.Kind,
			Name:         s.Name,
			Host:         s.Host,
			Port:         s.Port,
			Username:     s.Username,
			AuthType:     s.AuthType,
			KeyPath:      s.KeyPath,
			JumpServerID: s.JumpServerID,
			Tags:         s.Tags,
			Favorite:     s.Favorite,
			HostKey:      s.HostKey,
			TrustLevel:   s.TrustLevel,
		}
		if opt.IncludeSecrets && (s.HasPassword || s.HasPassphrase) {
			// Decrypted here and re-encrypted under the passphrase by Seal. It
			// is the only moment a secret exists in the clear, and it never
			// reaches disk in that state.
			pw, pp, err := st.Secrets(s.ID)
			if err != nil {
				return Bundle{}, fmt.Errorf("%s: %w", s.Name, err)
			}
			h.Password, h.Passphrase = pw, pp
		}
		b.Hosts = append(b.Hosts, h)
	}

	workspaces, err := st.ListWorkspaces()
	if err != nil {
		return Bundle{}, err
	}
	for _, w := range workspaces {
		b.Workspaces = append(b.Workspaces, Workspace{
			ID:                  w.ID,
			ProjectID:           w.ProjectID,
			ServerID:            w.ServerID,
			Name:                w.Name,
			RemotePath:          w.RemotePath,
			DefaultTmuxSession:  w.DefaultTmuxSession,
			DefaultAgentCommand: w.DefaultAgentCommand,
			Env:                 w.Env,
			Sort:                w.Sort,
		})
	}

	agents, err := st.ListAgents()
	if err != nil {
		return Bundle{}, err
	}
	for _, a := range agents {
		b.Agents = append(b.Agents, Agent{
			ID:          a.ID,
			WorkspaceID: a.WorkspaceID,
			Name:        a.Name,
			Command:     a.Command,
			TmuxSession: a.TmuxSession,
		})
	}

	if opt.IncludeLibrary {
		skills, err := st.ListSkills(store.SkillFilter{})
		if err != nil {
			return Bundle{}, err
		}
		for _, sk := range skills {
			b.Skills = append(b.Skills, skill.ToPortable(sk))
		}

		b.Settings = map[string]string{}
		for _, k := range TravellingSettings {
			if v := st.GetSetting(k, ""); v != "" {
				b.Settings[k] = v
			}
		}
	}

	return b, nil
}
