package integration

import (
	"testing"

	"agentmux/internal/agentkit"
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
