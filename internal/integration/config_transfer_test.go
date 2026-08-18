package integration

import (
	"path/filepath"
	"testing"

	"agentmux/internal/app"
	"agentmux/internal/portable"
	"agentmux/internal/store"
)

// newInstallation builds a whole app core against a database of its own, which
// is what "another machine" means for a test: a separate data directory, a
// separate master key, and no memory of the first one.
func newInstallation(t *testing.T) *app.Core {
	t.Helper()
	t.Setenv("AGENTMUX_DATA_DIR", t.TempDir())
	core, err := app.NewCore()
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	t.Cleanup(core.Shutdown)
	return core
}

// The whole point of the feature, exercised through the service the frontend
// calls: a configuration written on one machine opens on another, with the
// passwords usable rather than merely present.
func TestAConfigurationFileMovesAnInstallation(t *testing.T) {
	from := newInstallation(t)
	pw := "hunter2"
	srv, err := from.Store.SaveServer(store.ServerInput{
		Name:     "prod",
		Host:     "10.0.0.5",
		Port:     22,
		Username: "deploy",
		AuthType: store.AuthPassword,
		Password: &pw,
	})
	if err != nil {
		t.Fatalf("save server: %v", err)
	}
	project, err := from.Store.SaveProject(store.Project{Name: "checkout-service"})
	if err != nil {
		t.Fatalf("save project: %v", err)
	}
	ws, err := from.Store.SaveWorkspace(store.Workspace{
		ProjectID:  project.ID,
		ServerID:   srv.ID,
		Name:       "checkout",
		RemotePath: "/srv/checkout",
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	if _, err := app.NewAgentService(from).Save(store.Agent{
		WorkspaceID: ws.ID,
		Name:        "backend",
		Command:     "claude",
	}); err != nil {
		t.Fatalf("save agent: %v", err)
	}

	path := filepath.Join(t.TempDir(), "config.agentmux")
	manifest, err := app.NewConfigService(from).Export(path, "correct horse battery",
		portable.Options{IncludeSecrets: true, IncludeLibrary: true})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if manifest.Hosts != 1 || manifest.Projects != 1 || manifest.Agents != 1 || !manifest.HasSecrets {
		t.Fatalf("the export did not carry what it should: %+v", manifest)
	}

	to := newInstallation(t)
	svc := app.NewConfigService(to)

	// Reading the file is a separate step from importing it, so the dialog can
	// say what is inside before anything changes.
	seen, err := svc.Inspect(path, "correct horse battery")
	if err != nil {
		t.Fatalf("inspect: %v", err)
	}
	if seen.Hosts != 1 {
		t.Fatalf("inspect saw the wrong thing: %+v", seen)
	}
	if _, err := svc.Inspect(path, "not the passphrase"); err == nil {
		t.Fatal("the wrong passphrase inspected the file anyway")
	}

	res, err := svc.Import(path, "correct horse battery")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Hosts.Added != 1 || res.Workspaces.Added != 1 || res.Agents.Added != 1 {
		t.Fatalf("the import did not land: %+v", res)
	}

	servers, err := to.Store.ListServers()
	if err != nil {
		t.Fatalf("list servers: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected one host, got %d", len(servers))
	}
	// Decrypting here proves the secret was re-sealed under this installation's
	// own master key rather than carried in a form only the first one can read.
	got, _, err := to.Store.Secrets(servers[0].ID)
	if err != nil {
		t.Fatalf("secrets: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("the password did not survive the trip: %q", got)
	}

	agents, err := to.Store.ListAgents()
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 || agents[0].TmuxSession != "agentmux/checkout-service/backend" {
		t.Fatalf("the agent arrived without its session: %+v", agents)
	}
}
