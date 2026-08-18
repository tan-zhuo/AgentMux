package agentkit

import (
	"errors"
	"testing"

	"agentmux/internal/sshx"
)

func TestMethodWorksOnWindows(t *testing.T) {
	cases := []struct {
		script string
		want   bool
	}{
		{`npm install -g @anthropic-ai/claude-code`, true},
		{`pipx install aider-chat`, true},
		{`curl -fsSL https://claude.ai/install.sh | bash`, false},
		{`curl -fsSL https://opencode.ai/install | bash`, false},
		{`curl -LsSf https://astral.sh/uv/install.sh | sh`, false},
		{`sudo apt-get update && sudo apt-get install -y tmux`, false},
	}
	for _, c := range cases {
		if got := methodWorksOnWindows(Method{Script: c.script}); got != c.want {
			t.Errorf("methodWorksOnWindows(%q) = %v, want %v", c.script, got, c.want)
		}
	}
}

func TestDetectWindowsParsesAndSkipsTmux(t *testing.T) {
	// A host with node and npm present: the npm agents become installable, and
	// tmux — meaningless where sessions live in AgentMux's own daemon — must
	// not be offered at all.
	rep := DetectWindows(stubRunner{out: "os~@~Microsoft Windows 11 Pro\n" +
		"shell~@~powershell\n" +
		"bin~@~node~@~C:\\Program Files\\nodejs\\node.exe\n" +
		"bin~@~npm~@~C:\\Program Files\\nodejs\\npm.cmd\n"}, "srv")
	if rep.Error != "" {
		t.Fatalf("unexpected error: %s", rep.Error)
	}
	if rep.OS != "Microsoft Windows 11 Pro" || rep.Shell != "powershell" {
		t.Fatalf("identity parsed wrong: %q %q", rep.OS, rep.Shell)
	}
	if !rep.Presence["npm"].Installed {
		t.Fatal("npm should be present")
	}
	seenClaude := false
	for _, st := range append(append([]ToolStatus{}, rep.Runtimes...), rep.Agents...) {
		if st.Tool.ID == "tmux" {
			t.Fatal("tmux has no meaning on a native Windows host and must not be offered")
		}
		if st.Tool.ID == "claude-code" {
			seenClaude = true
			if len(st.Available) != 1 || st.Available[0].ID != "npm" {
				t.Fatalf("claude-code on Windows should offer exactly the npm method, got %+v", st.Available)
			}
		}
	}
	if !seenClaude {
		t.Fatal("claude-code missing from the report")
	}
}

func TestDetectWindowsSurfacesErrors(t *testing.T) {
	rep := DetectWindows(stubRunner{err: errors.New("no powershell here")}, "srv")
	if rep.Error == "" {
		t.Fatal("a failing runner should surface an error")
	}
}

type stubRunner struct {
	out string
	err error
}

func (s stubRunner) Exec(_, _ string) (sshx.ExecResult, error) {
	if s.err != nil {
		return sshx.ExecResult{}, s.err
	}
	return sshx.ExecResult{Stdout: s.out}, nil
}
