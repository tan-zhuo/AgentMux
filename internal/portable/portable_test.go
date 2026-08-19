package portable

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"agentmux/internal/store"
)

// newStore opens a database of its own, so a test never sees another test's
// rows and never touches the real installation.
func newStore(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("AGENTMUX_DATA_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// seed builds a small installation: one host with a password, a project, a
// workspace on that host, and an agent in it.
func seed(t *testing.T, st *store.Store) {
	t.Helper()
	pw, pp := "hunter2", "key-phrase"
	srv, err := st.SaveServer(store.ServerInput{
		Name:       "prod",
		Host:       "10.0.0.5",
		Port:       22,
		Username:   "deploy",
		AuthType:   store.AuthPassword,
		Password:   &pw,
		Passphrase: &pp,
		Tags:       []string{"prod"},
		TrustLevel: store.TrustProduction,
	})
	if err != nil {
		t.Fatalf("save server: %v", err)
	}
	if err := st.PinHostKey(srv.ID, "ssh-ed25519 AAAA-pinned"); err != nil {
		t.Fatalf("pin host key: %v", err)
	}
	proj, err := st.SaveProject(store.Project{Name: "checkout-service"})
	if err != nil {
		t.Fatalf("save project: %v", err)
	}
	ws, err := st.SaveWorkspace(store.Workspace{
		ProjectID:           proj.ID,
		ServerID:            srv.ID,
		Name:                "checkout",
		RemotePath:          "/srv/checkout",
		DefaultAgentCommand: "claude",
		Env:                 map[string]string{"NODE_ENV": "production"},
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	if _, err := st.SaveAgent(store.Agent{
		WorkspaceID: ws.ID,
		Name:        "backend",
		Command:     "claude",
		TmuxSession: "agentmux/checkout/backend",
	}); err != nil {
		t.Fatalf("save agent: %v", err)
	}
}

func TestAConfigurationSurvivesTheTripToAnotherMachine(t *testing.T) {
	from := newStore(t)
	seed(t, from)

	bundle, err := Build(from, Options{IncludeSecrets: true, IncludeLibrary: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.agentmux")
	if err := Write(path, bundle, "correct horse battery"); err != nil {
		t.Fatalf("write: %v", err)
	}

	to := newStore(t)
	read, err := Read(path, "correct horse battery")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	res, err := Apply(context.Background(), to, nil, read, nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Hosts.Added != 1 || res.Projects.Added != 1 || res.Workspaces.Added != 1 || res.Agents.Added != 1 {
		t.Fatalf("expected one of each to land, got %+v", res)
	}

	servers, err := to.ListServers()
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected one host, got %d", len(servers))
	}
	got := servers[0]
	if got.Name != "prod" || got.Host != "10.0.0.5" || got.Username != "deploy" {
		t.Fatalf("the host arrived wrong: %+v", got)
	}
	if got.TrustLevel != store.TrustProduction {
		t.Fatalf("trust level did not travel: %q", got.TrustLevel)
	}
	if got.HostKey != "ssh-ed25519 AAAA-pinned" {
		t.Fatalf("the pinned host key did not travel: %q", got.HostKey)
	}
	if !got.HasPassword || !got.HasPassphrase {
		t.Fatalf("the secrets did not travel: %+v", got)
	}
	// The point of carrying secrets is that they are usable on arrival, which
	// means decrypting under the new machine's own master key.
	pw, pp, err := to.Secrets(got.ID)
	if err != nil {
		t.Fatalf("secrets: %v", err)
	}
	if pw != "hunter2" || pp != "key-phrase" {
		t.Fatalf("secrets came back wrong: %q / %q", pw, pp)
	}

	// The references have to point at the rows this machine made, not the ones
	// the file remembers.
	workspaces, err := to.ListWorkspaces()
	if err != nil {
		t.Fatalf("list workspaces: %v", err)
	}
	if len(workspaces) != 1 || workspaces[0].ServerID != got.ID {
		t.Fatalf("the workspace is not attached to the imported host: %+v", workspaces)
	}
	if workspaces[0].Env["NODE_ENV"] != "production" {
		t.Fatalf("the workspace environment did not travel: %+v", workspaces[0].Env)
	}
	agents, err := to.ListAgents()
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 || agents[0].WorkspaceID != workspaces[0].ID {
		t.Fatalf("the agent is not in the imported workspace: %+v", agents)
	}
}

func TestImportingTheSameFileTwiceAddsNothing(t *testing.T) {
	from := newStore(t)
	seed(t, from)
	bundle, err := Build(from, Options{IncludeSecrets: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	to := newStore(t)
	if _, err := Apply(context.Background(), to, nil, bundle, nil); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	res, err := Apply(context.Background(), to, nil, bundle, nil)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if res.Hosts.Added != 0 || res.Projects.Added != 0 || res.Workspaces.Added != 0 || res.Agents.Added != 0 {
		t.Fatalf("the second import should have added nothing, got %+v", res)
	}
	if res.Hosts.Skipped != 1 || res.Projects.Skipped != 1 || res.Workspaces.Skipped != 1 {
		t.Fatalf("the second import should have skipped what was there, got %+v", res)
	}
	servers, _ := to.ListServers()
	agents, _ := to.ListAgents()
	if len(servers) != 1 || len(agents) != 1 {
		t.Fatalf("importing twice duplicated rows: %d hosts, %d agents", len(servers), len(agents))
	}
}

func TestAnImportNeverOverwritesWhatIsAlreadyHere(t *testing.T) {
	from := newStore(t)
	seed(t, from)
	bundle, err := Build(from, Options{IncludeSecrets: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	to := newStore(t)
	local := "local-password"
	mine, err := to.SaveServer(store.ServerInput{
		Name:     "my prod",
		Host:     "10.0.0.5",
		Port:     22,
		Username: "deploy",
		AuthType: store.AuthPassword,
		Password: &local,
	})
	if err != nil {
		t.Fatalf("save server: %v", err)
	}
	if _, err := Apply(context.Background(), to, nil, bundle, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}

	servers, _ := to.ListServers()
	if len(servers) != 1 {
		t.Fatalf("the same address should have matched the existing host, got %d", len(servers))
	}
	if servers[0].Name != "my prod" {
		t.Fatalf("the local name was overwritten: %q", servers[0].Name)
	}
	pw, _, err := to.Secrets(mine.ID)
	if err != nil {
		t.Fatalf("secrets: %v", err)
	}
	if pw != "local-password" {
		t.Fatalf("the local password was overwritten: %q", pw)
	}
	// The imported workspace still had to land, attached to the host that was
	// already here.
	workspaces, _ := to.ListWorkspaces()
	if len(workspaces) != 1 || workspaces[0].ServerID != mine.ID {
		t.Fatalf("the workspace did not attach to the existing host: %+v", workspaces)
	}
}

func TestAnExportWithoutSecretsCarriesNone(t *testing.T) {
	from := newStore(t)
	seed(t, from)
	bundle, err := Build(from, Options{IncludeSecrets: false})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, h := range bundle.Hosts {
		if h.Password != "" || h.Passphrase != "" {
			t.Fatalf("a secret travelled in an export that was not asked for one: %+v", h)
		}
	}
	if bundle.HasSecrets {
		t.Fatal("the bundle claims to carry secrets")
	}

	to := newStore(t)
	res, err := Apply(context.Background(), to, nil, bundle, nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	servers, _ := to.ListServers()
	if len(servers) != 1 || servers[0].HasPassword {
		t.Fatalf("the host should have arrived without a password: %+v", servers)
	}
	if len(res.Notes) == 0 {
		t.Fatal("an import that cannot connect without a password should say so")
	}
}

func TestTheWrongPassphraseOpensNothing(t *testing.T) {
	from := newStore(t)
	seed(t, from)
	bundle, _ := Build(from, Options{IncludeSecrets: true})
	path := filepath.Join(t.TempDir(), "config.agentmux")
	if err := Write(path, bundle, "correct horse battery"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := Read(path, "wrong horse battery"); err == nil {
		t.Fatal("the wrong passphrase opened the file")
	}
}

func TestASealedFileHidesItsSecrets(t *testing.T) {
	from := newStore(t)
	seed(t, from)
	bundle, _ := Build(from, Options{IncludeSecrets: true})
	sealed, err := Seal(mustJSON(t, bundle), "correct horse battery", bundle.ExportedAt)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	for _, secret := range []string{"hunter2", "key-phrase", "10.0.0.5", "checkout-service"} {
		if strings.Contains(string(sealed), secret) {
			t.Fatalf("%q is readable in the sealed file", secret)
		}
	}
}

func TestATamperedHeaderIsRefused(t *testing.T) {
	plain := []byte(`{"format":"agentmux.config/v1"}`)
	sealed, err := Seal(plain, "correct horse battery", 1700000000)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	// Weakening the work factor in the header must not weaken the file: the
	// parameters are authenticated, so a rewritten header stops opening.
	weakened := strings.Replace(string(sealed), `"time": 3`, `"time": 1`, 1)
	if weakened == string(sealed) {
		t.Fatal("the test did not find the parameter it meant to change")
	}
	if _, err := Open([]byte(weakened), "correct horse battery"); err == nil {
		t.Fatal("a file with rewritten key derivation parameters still opened")
	}
}

func TestAShortPassphraseIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	if _, err := Seal([]byte("{}"), "short", 0); err == nil {
		t.Fatal("a five character passphrase was accepted")
	}
}

func TestSettingsOnlyFillWhatThisMachineHasNotDecided(t *testing.T) {
	from := newStore(t)
	if err := from.SetSetting("theme", "nord"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if err := from.SetSetting("language", "ja"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	bundle, err := Build(from, Options{IncludeLibrary: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	to := newStore(t)
	if err := to.SetSetting("theme", "gruvbox-dark"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	if _, err := Apply(context.Background(), to, nil, bundle, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := to.GetSetting("theme", ""); got != "gruvbox-dark" {
		t.Fatalf("the theme chosen on this machine was overwritten: %q", got)
	}
	if got := to.GetSetting("language", ""); got != "ja" {
		t.Fatalf("a setting this machine had not chosen should have been filled in, got %q", got)
	}
}

func TestTheOrchestratorIsNeverSwitchedOnByAFile(t *testing.T) {
	from := newStore(t)
	if err := from.SetSetting("orch.enabled", "true"); err != nil {
		t.Fatalf("set setting: %v", err)
	}
	bundle, err := Build(from, Options{IncludeLibrary: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, ok := bundle.Settings["orch.enabled"]; ok {
		t.Fatal("whether the orchestrator may act travelled in the file")
	}
}

func mustJSON(t *testing.T, b Bundle) []byte {
	t.Helper()
	out, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}

func TestAnAgentWhoseSessionIsTakenIsNamedByThisMachine(t *testing.T) {
	from := newStore(t)
	seed(t, from)
	bundle, err := Build(from, Options{IncludeSecrets: true})
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	// A machine that already runs something in that session name, under a
	// different project — so the agent itself is new but its session is not.
	to := newStore(t)
	srv, err := to.SaveServer(store.ServerInput{
		Name: "other", Host: "10.0.0.9", Port: 22, Username: "deploy", AuthType: store.AuthAgent,
	})
	if err != nil {
		t.Fatalf("save server: %v", err)
	}
	proj, err := to.SaveProject(store.Project{Name: "something-else"})
	if err != nil {
		t.Fatalf("save project: %v", err)
	}
	ws, err := to.SaveWorkspace(store.Workspace{
		ProjectID: proj.ID, ServerID: srv.ID, Name: "else", RemotePath: "/srv/else",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	if _, err := to.SaveAgent(store.Agent{
		WorkspaceID: ws.ID,
		Name:        "squatter",
		Command:     "claude",
		TmuxSession: "agentmux/checkout/backend",
	}); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	res, err := Apply(context.Background(), to, nil, bundle,
		func(workspaceID, agentName string) string { return "agentmux/named-here/" + agentName })
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if res.Agents.Added != 1 {
		t.Fatalf("the agent should still have landed, got %+v", res.Agents)
	}
	agents, _ := to.ListAgents()
	var landed store.Agent
	for _, a := range agents {
		if a.Name == "backend" {
			landed = a
		}
	}
	if landed.TmuxSession != "agentmux/named-here/backend" {
		t.Fatalf("the agent kept a session that was already taken: %q", landed.TmuxSession)
	}
	if len(res.Notes) == 0 {
		t.Fatal("a renamed session is worth saying out loud")
	}
}

// A kind of host this package has never heard of still has to be matched the
// way it deserves: by its address when it has one, by its kind when it does
// not. The literals here are deliberate — the point is that nothing in the
// import has to be taught about a new kind for this to hold.
func TestHostsAreTheSameMachineByAddressRatherThanByKindList(t *testing.T) {
	posix := hostKey("ssh", "10.0.0.5", 22, "deploy")
	windows := hostKey("sshwin", "10.0.0.5", 22, "deploy")
	if posix == windows {
		t.Fatal("the same box reached two ways should be two rows, not one")
	}
	if hostKey("ssh", "10.0.0.5", 22, "deploy") != hostKey("", "10.0.0.5", 22, "DEPLOY") {
		t.Fatal("an unset kind means ssh, and a username is not case sensitive")
	}
	if hostKey("local", "", 0, "") == hostKey("localwin", "", 0, "") {
		t.Fatal("the two flavours of this computer are two hosts")
	}
	if hostKey("local", "", 0, "") != hostKey("local", "", 0, "") {
		t.Fatal("there is only ever one host of a kind that has no address")
	}
}

// A remembered desktop belongs to one machine, and the store keys it by an id
// that is minted afresh on the other side. It has to arrive pointing at the
// same host anyway.
func TestAHostsOwnSettingsFollowItToItsNewID(t *testing.T) {
	from := newStore(t)
	seed(t, from)
	servers, _ := from.ListServers()
	if err := from.SetSetting("desktop."+servers[0].ID, "vnc:5901"); err != nil {
		t.Fatalf("set setting: %v", err)
	}

	bundle, err := Build(from, Options{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got := bundle.Hosts[0].Settings["desktop"]; got != "vnc:5901" {
		t.Fatalf("the host's own setting did not travel: %q", got)
	}

	to := newStore(t)
	if _, err := Apply(context.Background(), to, nil, bundle, nil); err != nil {
		t.Fatalf("apply: %v", err)
	}
	landed, _ := to.ListServers()
	if len(landed) != 1 {
		t.Fatalf("expected one host, got %d", len(landed))
	}
	if landed[0].ID == servers[0].ID {
		t.Fatal("the id was reused, so this test proves nothing")
	}
	if got := to.GetSetting("desktop."+landed[0].ID, ""); got != "vnc:5901" {
		t.Fatalf("the setting is not attached to the host that arrived: %q", got)
	}
}
