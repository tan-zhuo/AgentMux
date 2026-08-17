package localx

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"agentmux/internal/sshx"
)

// The local transport is the one part of AgentMux that can be exercised without a
// server to talk to, so these are real end-to-end tests of it: actual processes,
// an actual PTY, actual files. They skip on Windows, where the same code paths run
// through WSL and cannot be assumed present on a build machine.

func skipUnlessPOSIX(t *testing.T) *Runner {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the local transport goes through WSL on Windows, which a test host may not have")
	}
	run := NewRunner()
	if err := run.Available(); err != nil {
		t.Skipf("this machine cannot host anything: %v", err)
	}
	return run
}

func TestExecCapturesOutputAndExitCodes(t *testing.T) {
	run := skipUnlessPOSIX(t)

	res, err := run.Exec("", `printf 'out'; printf 'err' >&2`)
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if res.Stdout != "out" {
		t.Errorf("stdout = %q, want %q", res.Stdout, "out")
	}
	if res.Stderr != "err" {
		t.Errorf("stderr = %q, want %q", res.Stderr, "err")
	}
	if res.Code != 0 {
		t.Errorf("code = %d, want 0", res.Code)
	}

	// A non-zero exit is data, not an error: tmux existence checks are built on it.
	res, err = run.Exec("", "exit 3")
	if err != nil {
		t.Fatalf("exec of a failing command returned an error: %v", err)
	}
	if res.Code != 3 {
		t.Errorf("code = %d, want 3", res.Code)
	}
}

func TestHomeIsADirectory(t *testing.T) {
	run := skipUnlessPOSIX(t)

	home, err := run.Home()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	if !strings.HasPrefix(home, "/") {
		t.Fatalf("home = %q, want an absolute POSIX path", home)
	}
	local, err := hostPath(home)
	if err != nil {
		t.Fatalf("hostPath: %v", err)
	}
	st, err := os.Stat(local)
	if err != nil || !st.IsDir() {
		t.Fatalf("home %q is not a directory here", home)
	}
}

func TestTerminalRunsACommandAndEnds(t *testing.T) {
	run := skipUnlessPOSIX(t)
	terms := NewTerminals(run)

	opened, err := terms.OpenTerminal(sshx.ShellOptions{
		ServerID: "local",
		Cols:     80,
		Rows:     24,
		Command:  `printf 'hello from a pty'; exit 0`,
	})
	if err != nil {
		t.Fatalf("open terminal: %v", err)
	}
	t.Cleanup(func() { _ = opened.Session.Close() })

	// The PTY merges the streams, so there is one reader and no stderr.
	if opened.Stderr != nil {
		t.Error("a local terminal should not report a second stream")
	}

	got := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		var seen strings.Builder
		for {
			n, err := opened.Stdout.Read(buf)
			if n > 0 {
				seen.Write(buf[:n])
				if strings.Contains(seen.String(), "hello from a pty") {
					got <- seen.String()
					return
				}
			}
			if err != nil {
				got <- seen.String()
				return
			}
		}
	}()

	select {
	case out := <-got:
		if !strings.Contains(out, "hello from a pty") {
			t.Fatalf("terminal output = %q, want it to contain the command's output", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for output from the local terminal")
	}
}

func TestTerminalAppliesCwdAndEnvironment(t *testing.T) {
	run := skipUnlessPOSIX(t)
	terms := NewTerminals(run)

	dir := t.TempDir()
	opened, err := terms.OpenTerminal(sshx.ShellOptions{
		ServerID: "local",
		Cols:     80,
		Rows:     24,
		Cwd:      dir,
		Env:      map[string]string{"AGENTMUX_TEST": "yes"},
		Command:  `printf '%s|%s' "$PWD" "$AGENTMUX_TEST"`,
	})
	if err != nil {
		t.Fatalf("open terminal: %v", err)
	}
	t.Cleanup(func() { _ = opened.Session.Close() })

	deadline := time.After(10 * time.Second)
	read := make(chan string, 1)
	go func() {
		buf := make([]byte, 4096)
		var seen strings.Builder
		for {
			n, err := opened.Stdout.Read(buf)
			if n > 0 {
				seen.Write(buf[:n])
				if strings.Contains(seen.String(), "|yes") {
					read <- seen.String()
					return
				}
			}
			if err != nil {
				read <- seen.String()
				return
			}
		}
	}()

	select {
	case out := <-read:
		// macOS reports /var as /private/var, so the tail is what matters.
		if !strings.Contains(out, filepath.Base(dir)) || !strings.Contains(out, "|yes") {
			t.Fatalf("terminal output = %q, want the working directory and the variable", out)
		}
	case <-deadline:
		t.Fatal("timed out waiting for the terminal to report its directory")
	}
}

func TestFilesRoundTrip(t *testing.T) {
	run := skipUnlessPOSIX(t)
	files := NewFiles(run)

	dir := t.TempDir()
	sub := filepath.ToSlash(filepath.Join(dir, "nested"))
	if err := files.Mkdir("local", sub); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	target := sub + "/note.txt"
	written, err := files.WriteFile("local", target, "first\n", 0, false)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if written.ModTime == 0 {
		t.Error("write returned no modification time, which is the baseline the editor needs")
	}
	if written.Content != "" {
		t.Error("write echoed the content back, which the editor does not need")
	}

	read, err := files.ReadFile("local", target)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if read.Content != "first\n" {
		t.Errorf("content = %q, want %q", read.Content, "first\n")
	}

	// The modification-time guard is the same promise the SFTP editor makes.
	time.Sleep(1100 * time.Millisecond)
	if _, err := files.WriteFile("local", target, "outside change\n", 0, false); err != nil {
		t.Fatalf("second write: %v", err)
	}
	if _, err := files.WriteFile("local", target, "clobber\n", read.ModTime, false); err == nil {
		t.Error("writing over a newer file was allowed; the modification-time guard is not working")
	}

	listing, err := files.List("local", sub)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listing.Entries) != 1 || listing.Entries[0].Name != "note.txt" {
		t.Fatalf("listing = %+v, want one entry named note.txt", listing.Entries)
	}
	if listing.Path != sub || listing.Parent != filepath.ToSlash(dir) {
		t.Errorf("listing path/parent = %q/%q, want %q/%q", listing.Path, listing.Parent, sub, dir)
	}

	moved := sub + "/renamed.txt"
	if err := files.Rename("local", target, moved); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := files.ReadFile("local", target); err == nil {
		t.Error("the old path still reads after a rename")
	}
	if err := files.Remove("local", moved, false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := files.Remove("local", sub, true); err != nil {
		t.Fatalf("remove directory: %v", err)
	}
}

func TestReadFileRefusesBinariesAndDirectories(t *testing.T) {
	run := skipUnlessPOSIX(t)
	files := NewFiles(run)

	dir := t.TempDir()
	bin := filepath.Join(dir, "blob.bin")
	if err := os.WriteFile(bin, []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := files.ReadFile("local", filepath.ToSlash(bin)); err == nil {
		t.Error("a file with a NUL byte was offered to the editor")
	}
	if _, err := files.ReadFile("local", filepath.ToSlash(dir)); err == nil {
		t.Error("a directory was offered to the editor")
	}
}

func TestProbeReportsThisMachine(t *testing.T) {
	run := skipUnlessPOSIX(t)

	p := run.Probe()
	if !p.OK {
		t.Fatalf("probe failed: %s", p.Error)
	}
	if p.OS == "" {
		t.Error("probe reported no operating system")
	}
	// tmux may or may not be installed on a build machine; what matters is that
	// the answer is consistent with what the shell says.
	res, err := run.Exec("", "command -v tmux >/dev/null")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if (res.Code == 0) != p.HasTmux {
		t.Errorf("probe says tmux=%v, but the shell disagrees", p.HasTmux)
	}
}
