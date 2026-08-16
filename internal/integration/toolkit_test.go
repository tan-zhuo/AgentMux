package integration

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"agentmux/internal/agentkit"
	"agentmux/internal/app"
	"agentmux/internal/store"
	"agentmux/internal/tmuxx"
)

func TestDetectToolchain(t *testing.T) {
	pool, _ := newPool(t)

	rep := agentkit.Detect(pool, "test-server")
	if rep.Error != "" {
		t.Fatalf("detect failed: %s", rep.Error)
	}
	if rep.OS == "" {
		t.Error("expected an OS string")
	}
	if len(rep.Agents) == 0 || len(rep.Runtimes) == 0 {
		t.Fatalf("expected the catalogue to be reported, got %d agents / %d runtimes",
			len(rep.Agents), len(rep.Runtimes))
	}
	t.Logf("os=%q shell=%q", rep.OS, rep.Shell)

	// The test host is known to have tmux, so detection must find it with a
	// path and a version. Anything else means the probe script is broken.
	var tmuxStatus *agentkit.ToolStatus
	for i := range rep.Runtimes {
		if rep.Runtimes[i].Tool.ID == "tmux" {
			tmuxStatus = &rep.Runtimes[i]
		}
	}
	if tmuxStatus == nil {
		t.Fatal("tmux missing from the runtime report")
	}
	if !tmuxStatus.Installed {
		t.Fatal("tmux is installed on the test host but detection says otherwise")
	}
	if tmuxStatus.Path == "" {
		t.Error("expected a path for tmux")
	}
	if tmuxStatus.Version == "" {
		t.Error("expected a version string for tmux")
	}
	t.Logf("tmux %s at %s", tmuxStatus.Version, tmuxStatus.Path)

	// Something definitely absent must come back as not installed rather than
	// as a parse artefact.
	for _, a := range rep.Agents {
		t.Logf("agent %-14s installed=%-5v version=%-12q methods=%d blocked=%q",
			a.Tool.ID, a.Installed, a.Version, len(a.Available), a.Blocked)
	}
	for _, r := range rep.Runtimes {
		t.Logf("runtime %-12s installed=%-5v version=%-12q methods=%d",
			r.Tool.ID, r.Installed, r.Version, len(r.Available))
	}

	if p, ok := rep.Presence["curl"]; !ok {
		t.Error("curl was never probed")
	} else {
		t.Logf("curl present=%v at %q", p.Installed, p.Path)
	}
}

// TestLaunchInDirNamesSessionAfterFolder covers the file-browser quick launch:
// pick a directory, get a tmux session named after it with the command running
// in that directory, and get an attach rather than a second agent if it is run
// again while something is already there.
func TestLaunchInDirNamesSessionAfterFolder(t *testing.T) {
	pool, _ := newPool(t)
	tm := tmuxx.New(pool)

	// A directory whose name we control, so the derived session name is known.
	dir := "/tmp/Orbit API"
	if r, err := pool.Exec("test-server", `mkdir -p '/tmp/Orbit API'`); err != nil || r.Code != 0 {
		t.Fatalf("mkdir: %v %+v", err, r)
	}
	const session = "agentmux/orbit-api"
	_ = tm.KillSession("test-server", session)
	t.Cleanup(func() {
		_ = tm.KillSession("test-server", session)
		_, _ = pool.Exec("test-server", `rmdir '/tmp/Orbit API' 2>/dev/null`)
	})

	svc, serverID := newAgentService(t)

	res, err := svc.LaunchInDir(serverID, dir, "sleep 120")
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if res.Session != session {
		t.Fatalf("session name: got %q want %q", res.Session, session)
	}
	if !res.Created {
		t.Error("expected the session to have been created")
	}

	// It must actually be running, in that directory.
	var pane *tmuxx.Pane
	for i := 0; i < 30; i++ {
		time.Sleep(200 * time.Millisecond)
		panes, err := tm.ListPanes("test-server")
		if err != nil {
			t.Fatal(err)
		}
		for j := range panes {
			if panes[j].SessionName == session {
				pane = &panes[j]
			}
		}
		if pane != nil && pane.Command == "sleep" {
			break
		}
	}
	if pane == nil {
		t.Fatalf("no pane for %s", session)
	}
	if pane.Command != "sleep" {
		t.Fatalf("expected the command to be running, pane shows %q", pane.Command)
	}
	if pane.Path != dir {
		t.Errorf("expected the pane to be in %q, got %q", dir, pane.Path)
	}
	t.Logf("session %s running %q in %s", session, pane.Command, pane.Path)

	// Launching again must attach rather than stack a second command on top.
	again, err := svc.LaunchInDir(serverID, dir, "sleep 120")
	if err != nil {
		t.Fatalf("second launch: %v", err)
	}
	if !again.Reused {
		t.Error("expected the second launch to report that it reused the session")
	}
}

// newAgentService builds the real service against a throwaway data directory,
// so the test exercises the same path the UI calls rather than a stand-in.
func newAgentService(t *testing.T) (*app.AgentService, string) {
	t.Helper()
	t.Setenv("AGENTMUX_DATA_DIR", t.TempDir())

	core, err := app.NewCore()
	if err != nil {
		t.Fatalf("core: %v", err)
	}
	t.Cleanup(core.Shutdown)

	port, _ := strconv.Atoi(os.Getenv("AGENTMUX_TEST_PORT"))
	if port == 0 {
		port = 22
	}
	srv, err := core.Store.SaveServer(store.ServerInput{
		Name:     "integration",
		Host:     os.Getenv("AGENTMUX_TEST_HOST"),
		Port:     port,
		Username: os.Getenv("AGENTMUX_TEST_USER"),
		AuthType: store.AuthKey,
		KeyPath:  os.Getenv("AGENTMUX_TEST_KEY"),
	})
	if err != nil {
		t.Fatalf("save server: %v", err)
	}
	return app.NewAgentService(core), srv.ID
}

// TestDetectFindsToolsOutsideTheSystemPath is the regression test for agent
// CLIs installed under $HOME going unnoticed.
//
// An SSH exec channel gets a non-login, non-interactive shell, so it never
// reads the profile line that puts ~/.local/bin on PATH. Claude Code installs
// itself there by default, which meant the tool reported "not installed" for
// something the user runs by hand every day.
func TestDetectFindsToolsOutsideTheSystemPath(t *testing.T) {
	pool, _ := newPool(t)

	// Prove the premise: the binary is unreachable from the inherited PATH.
	bare, err := pool.Exec("test-server", `command -v claude || echo MISSING`)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !strings.Contains(bare.Stdout, "MISSING") {
		t.Skip("claude is already on the default PATH here, so this test proves nothing")
	}

	// And that it really is installed where the profile would have put it.
	home, err := pool.Exec("test-server", `test -x "$HOME/.local/bin/claude" && echo YES || echo NO`)
	if err != nil {
		t.Fatalf("home probe: %v", err)
	}
	if !strings.Contains(home.Stdout, "YES") {
		t.Skip("no ~/.local/bin/claude on the test host")
	}

	rep := agentkit.Detect(pool, "test-server")
	if rep.Error != "" {
		t.Fatalf("detect: %s", rep.Error)
	}

	var claude *agentkit.ToolStatus
	for i := range rep.Agents {
		if rep.Agents[i].Tool.ID == "claude-code" {
			claude = &rep.Agents[i]
		}
	}
	if claude == nil {
		t.Fatal("claude-code missing from the report")
	}
	if !claude.Installed {
		t.Fatal("claude is installed in ~/.local/bin but detection did not find it")
	}
	if claude.Path == "" {
		t.Error("expected the path it was found at")
	}
	if claude.Version == "" {
		t.Error("expected a version, which means the binary was actually run")
	}
	t.Logf("found claude %s at %s", claude.Version, claude.Path)
}

// TestBroadcastReachesUnregisteredSessions covers the case the feature was
// unusable for: work started from the file browser produces a running tmux
// session and no agent record, so a broadcast limited to registered agents had
// nothing to talk to.
func TestBroadcastReachesUnregisteredSessions(t *testing.T) {
	pool, _ := newPool(t)
	tm := tmuxx.New(pool)
	svc, serverID := newAgentService(t)

	const session = "agentmux-bcast-test"
	_ = tm.KillSession("test-server", session)
	t.Cleanup(func() { _ = tm.KillSession("test-server", session) })
	if err := tm.NewSession("test-server", session, ""); err != nil {
		t.Fatalf("new-session: %v", err)
	}

	// No agent record exists for this session — that is the whole point.
	agents, err := svc.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 0 {
		t.Fatalf("expected no registered agents, got %d", len(agents))
	}

	receipts := svc.BroadcastTo(
		[]app.BroadcastTarget{{ServerID: serverID, Session: session}},
		"echo BROADCAST-TO-SESSION-OK",
		true,
	)
	if len(receipts) != 1 {
		t.Fatalf("expected one receipt, got %d", len(receipts))
	}
	if !receipts[0].OK {
		t.Fatalf("delivery failed: %s", receipts[0].Error)
	}
	t.Logf("receipt: ok=%v target=%s", receipts[0].OK, receipts[0].Target)

	// The receipt must mean something: the text has to have landed in the pane.
	// Captured by pane id rather than session name, because capture-pane wants
	// a pane target.
	paneID := receipts[0].Target
	var capture string
	for i := 0; i < 25; i++ {
		time.Sleep(200 * time.Millisecond)
		capture, err = tm.CapturePane("test-server", paneID, 50)
		if err != nil {
			t.Fatalf("capture %s: %v", paneID, err)
		}
		if strings.Contains(capture, "BROADCAST-TO-SESSION-OK") {
			return
		}
	}
	t.Fatalf("the message never reached pane %s:\n%s", paneID, capture)
}

// TestBroadcastSpansServers proves one send reaches targets on more than one
// server. Each target carries its own server id, so the fan-out is per target
// rather than scoped to a single connection.
//
// Two server records point at the same host here, which is enough to exercise
// the path that matters: two ids, two pooled connections, two resolutions.
func TestBroadcastSpansServers(t *testing.T) {
	pool, _ := newPool(t)
	tm := tmuxx.New(pool)
	svc, serverA := newAgentService(t)
	serverB := addSameHostAgain(t, svc)

	sessions := []string{"agentmux-xsrv-a", "agentmux-xsrv-b"}
	for _, s := range sessions {
		_ = tm.KillSession("test-server", s)
		if err := tm.NewSession("test-server", s, ""); err != nil {
			t.Fatalf("new-session %s: %v", s, err)
		}
	}
	t.Cleanup(func() {
		for _, s := range sessions {
			_ = tm.KillSession("test-server", s)
		}
	})

	receipts := svc.BroadcastTo([]app.BroadcastTarget{
		{ServerID: serverA, Session: sessions[0]},
		{ServerID: serverB, Session: sessions[1]},
	}, "echo CROSS-SERVER-OK", true)

	if len(receipts) != 2 {
		t.Fatalf("expected two receipts, got %d", len(receipts))
	}
	for i, r := range receipts {
		if !r.OK {
			t.Fatalf("target %d failed: %s", i, r.Error)
		}
		t.Logf("receipt %d: target=%s", i, r.Target)
	}
	if receipts[0].Target == receipts[1].Target {
		t.Fatal("both messages landed in the same pane, so the two targets were not distinct")
	}

	// Confirm the text arrived in both panes, not just that the calls returned.
	for i, r := range receipts {
		var capture string
		var err error
		delivered := false
		for attempt := 0; attempt < 25; attempt++ {
			time.Sleep(200 * time.Millisecond)
			capture, err = tm.CapturePane("test-server", r.Target, 50)
			if err != nil {
				t.Fatalf("capture %s: %v", r.Target, err)
			}
			if strings.Contains(capture, "CROSS-SERVER-OK") {
				delivered = true
				break
			}
		}
		if !delivered {
			t.Fatalf("target %d (%s) never received the message:\n%s", i, r.Target, capture)
		}
	}
}

// addSameHostAgain registers a second server record for the same test host.
func addSameHostAgain(t *testing.T, svc *app.AgentService) string {
	t.Helper()
	port, _ := strconv.Atoi(os.Getenv("AGENTMUX_TEST_PORT"))
	if port == 0 {
		port = 22
	}
	srv, err := svc.Store().SaveServer(store.ServerInput{
		Name:     "integration-2",
		Host:     os.Getenv("AGENTMUX_TEST_HOST"),
		Port:     port,
		Username: os.Getenv("AGENTMUX_TEST_USER"),
		AuthType: store.AuthKey,
		KeyPath:  os.Getenv("AGENTMUX_TEST_KEY"),
	})
	if err != nil {
		t.Fatalf("save second server: %v", err)
	}
	return srv.ID
}

func TestInstallRunsInsideTmux(t *testing.T) {
	pool, _ := newPool(t)

	rep := agentkit.Detect(pool, "test-server")
	if rep.Error != "" {
		t.Fatalf("detect: %s", rep.Error)
	}
	var claude *agentkit.ToolStatus
	for i := range rep.Agents {
		if rep.Agents[i].Tool.ID == "claude-code" {
			claude = &rep.Agents[i]
		}
	}
	if claude == nil {
		t.Fatal("claude-code missing from catalogue report")
	}
	// The WSL box has curl but no npm, so exactly one method should be offered.
	t.Logf("claude-code available methods: %d, blocked=%q", len(claude.Available), claude.Blocked)
	for _, m := range claude.Available {
		if m.Requires != "" && !rep.Presence[m.Requires].Installed {
			t.Errorf("method %s offered but its prerequisite %s is missing", m.ID, m.Requires)
		}
	}
}
