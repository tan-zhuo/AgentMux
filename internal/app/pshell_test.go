package app

import (
	"strings"
	"testing"

	"agentmux/internal/store"
)

func TestBuildLaunchCommandWin(t *testing.T) {
	ws := store.Workspace{
		RemotePath: "C:/Users/dev/proj",
		Env:        map[string]string{"API_KEY": "it's secret", "MODE": "prod"},
	}
	got := buildLaunchCommandWin(ws, "claude")
	want := `$env:API_KEY = 'it''s secret'; $env:MODE = 'prod'; Set-Location 'C:/Users/dev/proj'; claude`
	if got != want {
		t.Fatalf("buildLaunchCommandWin:\n got %q\nwant %q", got, want)
	}
}

func TestBuildLaunchCommandWinBare(t *testing.T) {
	got := buildLaunchCommandWin(store.Workspace{}, "codex")
	if got != "codex" {
		t.Fatalf("a workspace with nothing to set should launch the bare command, got %q", got)
	}
}

func TestPsQuote(t *testing.T) {
	if got := psQuote(`a'b`); got != `'a''b'` {
		t.Fatalf("psQuote = %q", got)
	}
}

func TestShellCommandsKnowWindowsShells(t *testing.T) {
	// The native session daemon reports "powershell"/"pwsh" the way tmux panes
	// report "bash"; if these fall out of the map, every idle native session
	// shows as a running agent and double-launch protection fires on prompts.
	for _, sh := range []string{"powershell", "pwsh", "cmd", "bash"} {
		if !shellCommands[sh] {
			t.Fatalf("shellCommands should treat %q as a prompt", sh)
		}
	}
	if shellCommands["claude"] || strings.TrimSpace("") != "" {
		t.Fatal("an agent binary must not read as a prompt")
	}
}
