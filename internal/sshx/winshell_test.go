package sshx

import (
	"strings"
	"testing"
)

func TestEncodePowerShell(t *testing.T) {
	// "ab" in UTF-16LE is 61 00 62 00, which is "YQBiAA==" in base64 — the exact
	// form `powershell -EncodedCommand` decodes.
	if got := EncodePowerShell("ab"); got != "YQBiAA==" {
		t.Fatalf("EncodePowerShell(ab) = %q", got)
	}
}

func TestPowerShellCommandIsShellNeutral(t *testing.T) {
	cmd := PowerShellCommand(`Write-Output "hello 'world'"`)
	// The whole point: nothing in the composed line needs quoting under either
	// cmd.exe or PowerShell, no matter what the script contains.
	for _, forbidden := range []string{`"`, `'`, `$`, `|`, `&`, `<`, `>`} {
		if strings.Contains(cmd, forbidden) {
			t.Fatalf("command line contains %q, which a default shell could interpret: %s", forbidden, cmd)
		}
	}
	if !strings.HasPrefix(cmd, "powershell.exe ") || !strings.Contains(cmd, "-EncodedCommand ") {
		t.Fatalf("unexpected shape: %s", cmd)
	}
}

func TestWinCommandLine(t *testing.T) {
	// A plain shell stays a shell request, exactly as on POSIX.
	if got := WinCommandLine(ShellOptions{}); got != "" {
		t.Fatalf("plain shell should be empty, got %q", got)
	}
	// cwd and environment without a command keeps the shell interactive.
	line := WinCommandLine(ShellOptions{Cwd: "C:/work", Env: map[string]string{"A": "1"}})
	if !strings.Contains(line, "-NoExit") {
		t.Fatalf("setup-only line must keep the shell open: %s", line)
	}
	// A command runs and ends the terminal with it.
	line = WinCommandLine(ShellOptions{Command: "npm install"})
	if strings.Contains(line, "-NoExit") {
		t.Fatalf("a command line must exit when done: %s", line)
	}
	if !strings.Contains(line, "-EncodedCommand ") {
		t.Fatalf("command must ride encoded: %s", line)
	}
}
