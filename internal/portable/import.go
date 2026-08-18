package portable

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agentmux/internal/skill"
	"agentmux/internal/store"
)

// SessionNamer produces the conventional tmux session name for an agent, so
// one that arrives without a usable one is named the way this machine names
// them rather than left with nothing. Naming lives in exactly one place, and it
// is not this package.
type SessionNamer func(workspaceID, agentName string) string

// Apply merges a bundle into this installation.
//
// Nothing here overwrites anything. A row that is already present — the same
// host, the same project, the same path on the same machine — is left exactly
// as it is and counted as skipped, and the rows that point at it are pointed at
// the copy that was already here. That makes importing the same file twice a
// no-op rather than a way to end up with two of everything, and it makes an
// import safe to try: the worst case is that it adds nothing.
//
// Ids are never reused. The file's ids only serve to rebuild the references
// between its own rows; every row that lands here is given an id by this
// machine's store.
func Apply(ctx context.Context, st *store.Store, skills *skill.Manager, b Bundle, name SessionNamer) (Result, error) {
	if b.Format != "" && b.Format != FileFormat {
		return Result{}, fmt.Errorf("unknown configuration format %q", b.Format)
	}
	res := Result{Notes: []string{}}

	folderIDs, err := importFolders(st, b, &res)
	if err != nil {
		return res, err
	}
	projectIDs, err := importProjects(st, b, folderIDs, &res)
	if err != nil {
		return res, err
	}
	hostIDs, err := importHosts(st, b, &res)
	if err != nil {
		return res, err
	}
	workspaceIDs, err := importWorkspaces(st, b, projectIDs, hostIDs, &res)
	if err != nil {
		return res, err
	}
	if err := importAgents(st, b, workspaceIDs, name, &res); err != nil {
		return res, err
	}
	if skills != nil {
		if err := importSkills(ctx, st, skills, b, projectIDs, &res); err != nil {
			return res, err
		}
	}
	if err := importSettings(st, b, &res); err != nil {
		return res, err
	}
	return res, nil
}

// importFolders walks parents before children, so a nested folder always has
// somewhere to land. A folder whose parent never arrives is hung at the root
// rather than dropped: losing a project because its folder went missing would
// be the worst possible trade.
func importFolders(st *store.Store, b Bundle, res *Result) (map[string]string, error) {
	existing, err := st.ListFolders()
	if err != nil {
		return nil, err
	}
	ids := map[string]string{}
	byKey := map[string]string{}
	for _, f := range existing {
		byKey[folderKey(f.Name, f.ParentID)] = f.ID
	}

	for _, f := range parentsFirst(b.Folders) {
		var parent *string
		if f.ParentID != nil {
			if mapped, ok := ids[*f.ParentID]; ok {
				parent = &mapped
			}
		}
		if id, ok := byKey[folderKey(f.Name, parent)]; ok {
			ids[f.ID] = id
			res.Folders.Skipped++
			continue
		}
		saved, err := st.SaveFolder(store.Folder{Name: f.Name, ParentID: parent, Sort: f.Sort})
		if err != nil {
			return nil, err
		}
		ids[f.ID] = saved.ID
		byKey[folderKey(f.Name, parent)] = saved.ID
		res.Folders.Added++
	}
	return ids, nil
}

func importProjects(st *store.Store, b Bundle, folderIDs map[string]string, res *Result) (map[string]string, error) {
	existing, err := st.ListProjects()
	if err != nil {
		return nil, err
	}
	byName := map[string]string{}
	for _, p := range existing {
		byName[fold(p.Name)] = p.ID
	}

	ids := map[string]string{}
	for _, p := range b.Projects {
		// A project is its name. Two people describing the same checkout will
		// not agree on the description or the sort order, and matching on those
		// would make an import produce a second "checkout-service".
		if id, ok := byName[fold(p.Name)]; ok {
			ids[p.ID] = id
			res.Projects.Skipped++
			continue
		}
		var folder *string
		if p.FolderID != nil {
			if mapped, ok := folderIDs[*p.FolderID]; ok {
				folder = &mapped
			}
		}
		saved, err := st.SaveProject(store.Project{
			Name:        p.Name,
			Description: p.Description,
			FolderID:    folder,
			Sort:        p.Sort,
		})
		if err != nil {
			return nil, err
		}
		ids[p.ID] = saved.ID
		byName[fold(p.Name)] = saved.ID
		res.Projects.Added++
	}
	return ids, nil
}

// importHosts lands the servers, then resolves the jump hosts in a second pass
// — a host may be reached through one that is later in the file, and a first
// pass that insisted otherwise would drop the link.
func importHosts(st *store.Store, b Bundle, res *Result) (map[string]string, error) {
	existing, err := st.ListServers()
	if err != nil {
		return nil, err
	}
	byIdentity := map[string]string{}
	for _, s := range existing {
		byIdentity[hostKey(s.Kind, s.Host, s.Port, s.Username)] = s.ID
		byIdentity[fold(s.Name)] = s.ID
	}

	ids := map[string]string{}
	fresh := map[string]Host{}
	for _, h := range b.Hosts {
		key := hostKey(h.Kind, h.Host, h.Port, h.Username)
		if id, ok := byIdentity[key]; ok {
			ids[h.ID] = id
			res.Hosts.Skipped++
			continue
		}
		if id, ok := byIdentity[fold(h.Name)]; ok {
			ids[h.ID] = id
			res.Hosts.Skipped++
			continue
		}
		in := store.ServerInput{
			Kind:       h.Kind,
			Name:       h.Name,
			Host:       h.Host,
			Port:       h.Port,
			Username:   h.Username,
			AuthType:   h.AuthType,
			KeyPath:    h.KeyPath,
			Tags:       h.Tags,
			Favorite:   h.Favorite,
			TrustLevel: h.TrustLevel,
			Password:   optional(h.Password),
			Passphrase: optional(h.Passphrase),
		}
		saved, err := st.SaveServer(in)
		if err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("%s: %v", nameOr(h.Name), err))
			continue
		}
		if h.HostKey != "" {
			if err := st.PinHostKey(saved.ID, h.HostKey); err != nil {
				return nil, err
			}
		}
		ids[h.ID] = saved.ID
		byIdentity[key] = saved.ID
		byIdentity[fold(h.Name)] = saved.ID
		fresh[h.ID] = h
		res.Hosts.Added++

		if h.AuthType == store.AuthKey && h.KeyPath != "" && !fileExists(h.KeyPath) {
			// The path came from the machine that wrote the file. Saying so now
			// beats finding out at the first connection, which is where it
			// would otherwise surface as a failure with no obvious cause.
			res.Notes = append(res.Notes,
				fmt.Sprintf("%s: the key file %s is not on this machine", h.Name, h.KeyPath))
		}
		if !b.HasSecrets && h.AuthType == store.AuthPassword {
			res.Notes = append(res.Notes,
				fmt.Sprintf("%s: exported without passwords — it will ask for one", h.Name))
		}
	}

	for oldID, h := range fresh {
		if h.JumpServerID == nil {
			continue
		}
		jump, ok := ids[*h.JumpServerID]
		if !ok {
			res.Notes = append(res.Notes,
				fmt.Sprintf("%s: its jump host is not in this file, so it now connects directly", h.Name))
			continue
		}
		if _, err := st.SaveServer(store.ServerInput{
			ID:           ids[oldID],
			Kind:         h.Kind,
			Name:         h.Name,
			Host:         h.Host,
			Port:         h.Port,
			Username:     h.Username,
			AuthType:     h.AuthType,
			KeyPath:      h.KeyPath,
			JumpServerID: &jump,
			Tags:         h.Tags,
			Favorite:     h.Favorite,
			TrustLevel:   h.TrustLevel,
		}); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

func importWorkspaces(st *store.Store, b Bundle, projectIDs, hostIDs map[string]string, res *Result) (map[string]string, error) {
	existing, err := st.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	byPath := map[string]string{}
	for _, w := range existing {
		byPath[w.ServerID+"\x1f"+w.RemotePath] = w.ID
	}

	ids := map[string]string{}
	for _, w := range b.Workspaces {
		project, okP := projectIDs[w.ProjectID]
		server, okS := hostIDs[w.ServerID]
		if !okP || !okS {
			// Its project or its host did not make it, so there is nothing to
			// hang it on. Counted rather than invented.
			res.Workspaces.Skipped++
			res.Notes = append(res.Notes,
				fmt.Sprintf("%s: skipped, its project or host is missing from the file", nameOr(w.Name)))
			continue
		}
		// A workspace is a path on a machine. The same path twice is the same
		// workspace, whatever either copy happens to be called.
		if id, ok := byPath[server+"\x1f"+w.RemotePath]; ok {
			ids[w.ID] = id
			res.Workspaces.Skipped++
			continue
		}
		saved, err := st.SaveWorkspace(store.Workspace{
			ProjectID:           project,
			ServerID:            server,
			Name:                w.Name,
			RemotePath:          w.RemotePath,
			DefaultTmuxSession:  w.DefaultTmuxSession,
			DefaultAgentCommand: w.DefaultAgentCommand,
			Env:                 w.Env,
			Sort:                w.Sort,
		})
		if err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("%s: %v", nameOr(w.Name), err))
			continue
		}
		ids[w.ID] = saved.ID
		byPath[server+"\x1f"+w.RemotePath] = saved.ID
		res.Workspaces.Added++
	}
	return ids, nil
}

func importAgents(st *store.Store, b Bundle, workspaceIDs map[string]string, name SessionNamer, res *Result) error {
	existing, err := st.ListAgents()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	sessions := map[string]bool{}
	for _, a := range existing {
		seen[a.WorkspaceID+"\x1f"+fold(a.Name)] = true
		if a.TmuxSession != "" {
			sessions[a.TmuxSession] = true
		}
	}

	for _, a := range b.Agents {
		ws, ok := workspaceIDs[a.WorkspaceID]
		if !ok {
			res.Agents.Skipped++
			continue
		}
		if seen[ws+"\x1f"+fold(a.Name)] {
			res.Agents.Skipped++
			continue
		}
		session := a.TmuxSession
		if session == "" || sessions[session] {
			// Two agents pointing at one session would fight over the same
			// pane, so a name already spoken for is not taken a second time.
			// The machine names the agent the way it names its own, and only
			// when that is taken too does the definition arrive without one —
			// which the next save fills in.
			taken := session
			session = ""
			if name != nil {
				if suggested := name(ws, a.Name); suggested != "" && !sessions[suggested] {
					session = suggested
				}
			}
			if taken != "" && session != taken {
				res.Notes = append(res.Notes,
					fmt.Sprintf("%s: the session %s is already taken here, so it was given %s",
						a.Name, taken, sessionOrNothing(session)))
			}
		}
		saved, err := st.SaveAgent(store.Agent{
			WorkspaceID: ws,
			Name:        a.Name,
			Command:     a.Command,
			TmuxSession: session,
		})
		if err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("%s: %v", nameOr(a.Name), err))
			continue
		}
		seen[ws+"\x1f"+fold(a.Name)] = true
		if saved.TmuxSession != "" {
			sessions[saved.TmuxSession] = true
		}
		res.Agents.Added++
	}
	return nil
}

// importSkills adds the skills this library does not have, matched by name.
// Project-scoped ones follow their projects to whatever ids they were given
// here; a scope pointing at a project that did not travel would silently never
// match anything.
func importSkills(ctx context.Context, st *store.Store, mgr *skill.Manager, b Bundle, projectIDs map[string]string, res *Result) error {
	existing, err := st.ListSkills(store.SkillFilter{})
	if err != nil {
		return err
	}
	byName := map[string]bool{}
	for _, sk := range existing {
		byName[fold(sk.Name)] = true
	}

	for _, p := range b.Skills {
		if byName[fold(p.Name)] {
			res.Skills.Skipped++
			continue
		}
		sk := p.Skill()
		if len(sk.ProjectIDs) > 0 {
			mapped := make([]string, 0, len(sk.ProjectIDs))
			for _, id := range sk.ProjectIDs {
				if to, ok := projectIDs[id]; ok {
					mapped = append(mapped, to)
				}
			}
			sk.ProjectIDs = mapped
		}
		if _, err := mgr.Create(ctx, sk); err != nil {
			res.Notes = append(res.Notes, fmt.Sprintf("%s: %v", nameOr(p.Name), err))
			continue
		}
		byName[fold(p.Name)] = true
		res.Skills.Added++
	}
	return nil
}

// importSettings fills in what this machine has not decided for itself. A
// preference already set here was set by the person sitting at it, and a file
// is not a reason to overrule them.
func importSettings(st *store.Store, b Bundle, res *Result) error {
	for _, k := range TravellingSettings {
		v, ok := b.Settings[k]
		if !ok || v == "" {
			continue
		}
		if st.GetSetting(k, "") != "" {
			res.Settings.Skipped++
			continue
		}
		if err := st.SetSetting(k, v); err != nil {
			return err
		}
		res.Settings.Added++
	}
	return nil
}

// Read opens a file from disk and returns what is inside it.
func Read(path, passphrase string) (Bundle, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, err
	}
	plain, err := Open(raw, passphrase)
	if err != nil {
		return Bundle{}, err
	}
	var b Bundle
	if err := json.Unmarshal(plain, &b); err != nil {
		return Bundle{}, fmt.Errorf("the file opened but its contents are not a configuration: %w", err)
	}
	return b, nil
}

// Write seals a bundle and puts it on disk, readable only by its owner.
func Write(path string, b Bundle, passphrase string) error {
	plain, err := json.Marshal(b)
	if err != nil {
		return err
	}
	sealed, err := Seal(plain, passphrase, b.ExportedAt)
	if err != nil {
		return err
	}
	return os.WriteFile(path, sealed, 0o600)
}

// --- helpers ----------------------------------------------------------------

// parentsFirst orders folders so that no folder is created before its parent.
func parentsFirst(folders []Folder) []Folder {
	depth := map[string]int{}
	byID := map[string]Folder{}
	for _, f := range folders {
		byID[f.ID] = f
	}
	var depthOf func(f Folder, guard int) int
	depthOf = func(f Folder, guard int) int {
		if f.ParentID == nil || guard > len(folders) {
			return 0
		}
		parent, ok := byID[*f.ParentID]
		if !ok {
			return 0
		}
		return 1 + depthOf(parent, guard+1)
	}
	out := append([]Folder(nil), folders...)
	for _, f := range out {
		depth[f.ID] = depthOf(f, 0)
	}
	sort.SliceStable(out, func(i, j int) bool { return depth[out[i].ID] < depth[out[j].ID] })
	return out
}

func folderKey(name string, parent *string) string {
	p := ""
	if parent != nil {
		p = *parent
	}
	return p + "\x1f" + fold(name)
}

// hostKey is what makes two rows the same machine.
//
// A host with an address is that address: the name is a label somebody chose,
// and two installs talking to the same box will not have chosen the same one.
// A host without an address is this computer in one flavour or another, so the
// kind is the identity and there is only ever one of each. Asking about the
// address rather than listing the kinds is what keeps this right when a new
// kind of host appears.
//
// The kind is part of the key either way: the same machine reached as a POSIX
// host and as a Windows one is two rows on purpose, not one row twice.
func hostKey(kind store.ServerKind, host string, port int, username string) string {
	if kind == "" {
		kind = store.KindSSH
	}
	if strings.TrimSpace(host) == "" {
		return "here\x1f" + string(kind)
	}
	return fmt.Sprintf("addr\x1f%s\x1f%s\x1f%d\x1f%s",
		kind, strings.ToLower(host), port, strings.ToLower(username))
}

func fold(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func sessionOrNothing(session string) string {
	if session == "" {
		return "none"
	}
	return session
}

func nameOr(name string) string {
	if strings.TrimSpace(name) == "" {
		return "(unnamed)"
	}
	return name
}

// optional turns a carried secret into the store's tri-state pointer: an empty
// one is left nil, which means "this row has no secret" rather than "clear it".
func optional(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	_, err := os.Stat(path)
	return err == nil
}
