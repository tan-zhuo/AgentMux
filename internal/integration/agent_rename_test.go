package integration

import (
	"os/exec"
	"testing"

	"agentmux/internal/app"
	"agentmux/internal/store"
)

// Renaming an agent has to reach the session it is attached to. These run
// against this computer as a host, so they need tmux here rather than a test
// server; without it there is nothing to rename and the test steps aside.
func newLocalAgentService(t *testing.T) (*app.AgentService, store.Workspace, string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed on this machine")
	}
	t.Setenv("AGENTMUX_DATA_DIR", t.TempDir())

	core, err := app.NewCore()
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	t.Cleanup(core.Shutdown)
	if err := core.Local.Available(); err != nil {
		t.Skipf("this computer cannot be used as a host here: %v", err)
	}

	srv, err := core.Store.SaveServer(store.ServerInput{Kind: store.KindLocal, Name: "this computer"})
	if err != nil {
		t.Fatalf("save server: %v", err)
	}
	project, err := core.Store.SaveProject(store.Project{Name: "Rename"})
	if err != nil {
		t.Fatalf("save project: %v", err)
	}
	ws, err := core.Store.SaveWorkspace(store.Workspace{
		ProjectID:  project.ID,
		ServerID:   srv.ID,
		Name:       "main",
		RemotePath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("save workspace: %v", err)
	}
	return app.NewAgentService(core), ws, project.Name
}

func TestRenamingAnAgentMovesTheSessionTheAppNamed(t *testing.T) {
	svc, ws, project := newLocalAgentService(t)

	agent, err := svc.Save(store.Agent{WorkspaceID: ws.ID, Name: "planner", Command: "sh"})
	if err != nil {
		t.Fatalf("save agent: %v", err)
	}
	if want := app.DefaultSessionName(project, "planner"); agent.TmuxSession != want {
		t.Fatalf("session name = %q, want %q", agent.TmuxSession, want)
	}

	// A live session, so the rename has to happen on the server and not only in
	// the database — a stored name with no session behind it strands the work.
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", agent.TmuxSession).CombinedOutput(); err != nil {
		t.Skipf("cannot start a tmux session here: %v: %s", err, out)
	}
	t.Cleanup(func() {
		_ = exec.Command("tmux", "kill-session", "-t", app.DefaultSessionName(project, "reviewer")).Run()
		_ = exec.Command("tmux", "kill-session", "-t", agent.TmuxSession).Run()
	})

	agent.Name = "reviewer"
	renamed, err := svc.Save(agent)
	if err != nil {
		t.Fatalf("rename agent: %v", err)
	}
	want := app.DefaultSessionName(project, "reviewer")
	if renamed.TmuxSession != want {
		t.Fatalf("session after rename = %q, want %q", renamed.TmuxSession, want)
	}
	if err := exec.Command("tmux", "has-session", "-t", "="+want).Run(); err != nil {
		t.Fatalf("tmux session %q does not exist after the rename: %v", want, err)
	}
}

func TestRenamingAnAgentLeavesASessionTheUserNamed(t *testing.T) {
	svc, ws, _ := newLocalAgentService(t)

	agent, err := svc.Save(store.Agent{
		WorkspaceID: ws.ID,
		Name:        "planner",
		Command:     "sh",
		TmuxSession: "my-own-session",
	})
	if err != nil {
		t.Fatalf("save agent: %v", err)
	}

	agent.Name = "reviewer"
	renamed, err := svc.Save(agent)
	if err != nil {
		t.Fatalf("rename agent: %v", err)
	}
	if renamed.TmuxSession != "my-own-session" {
		t.Fatalf("session after rename = %q, want it untouched", renamed.TmuxSession)
	}
}
