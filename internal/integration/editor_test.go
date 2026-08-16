package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"agentmux/internal/sftpx"
	"agentmux/internal/sshx"
)

// TestEditorReadWrite covers the path the online editor uses: read a file,
// change it, write it back, and refuse to write over a change someone else
// made in the meantime.
func TestEditorReadWrite(t *testing.T) {
	pool, _ := newPool(t)
	files := sftpx.New(pool, func(string, any) {})
	t.Cleanup(files.Close)

	home, err := files.Home("test-server")
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	dir := home + "/agentmux-editor-test"
	_ = files.Remove("test-server", dir, true)
	if err := files.Mkdir("test-server", dir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = files.Remove("test-server", dir, true) })

	// A script, so the executable bit gives us something to check.
	script := dir + "/run.sh"
	original := "#!/bin/sh\necho hello\n"
	if _, err := files.WriteFile("test-server", script, original, 0, false); err != nil {
		t.Fatalf("initial write: %v", err)
	}
	if err := runRemote(t, pool, "chmod 755 "+script); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	got, err := files.ReadFile("test-server", script)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Content != original {
		t.Fatalf("read back %q, want %q", got.Content, original)
	}
	if got.CRLF {
		t.Error("a LF file was reported as CRLF")
	}
	if !strings.Contains(got.Mode, "x") {
		t.Fatalf("expected an executable mode, got %s", got.Mode)
	}

	// Edit and save. The saved file must keep its executable bit — a script
	// that stops being runnable after an edit is a nasty surprise.
	edited := "#!/bin/sh\necho hello, edited\n"
	saved, err := files.WriteFile("test-server", script, edited, got.ModTime, got.CRLF)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(saved.Mode, "x") {
		t.Errorf("save dropped the executable bit: %s", saved.Mode)
	}
	again, err := files.ReadFile("test-server", script)
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if again.Content != edited {
		t.Fatalf("file on the server is %q, want %q", again.Content, edited)
	}

	// Someone else touches the file. Saving against the stale timestamp has to
	// fail rather than silently discard their work.
	time.Sleep(1100 * time.Millisecond) // mtime has one-second resolution
	if err := runRemote(t, pool, "echo 'from someone else' >> "+script); err != nil {
		t.Fatalf("external edit: %v", err)
	}
	if _, err := files.WriteFile("test-server", script, "clobbered\n", again.ModTime, false); err == nil {
		t.Fatal("expected a conflict error, the stale write went through")
	} else if !strings.Contains(err.Error(), "changed on the server") {
		t.Fatalf("unexpected error: %v", err)
	}
	// Passing 0 is the explicit "I know, overwrite it" path.
	if _, err := files.WriteFile("test-server", script, "clobbered\n", 0, false); err != nil {
		t.Fatalf("forced write: %v", err)
	}

	// CRLF files come back as LF and go back out as CRLF, so a Windows file
	// does not get silently rewritten by opening it.
	dos := dir + "/dos.txt"
	if err := runRemote(t, pool, "printf 'one\\r\\ntwo\\r\\n' > "+dos); err != nil {
		t.Fatalf("write CRLF file: %v", err)
	}
	d, err := files.ReadFile("test-server", dos)
	if err != nil {
		t.Fatalf("read CRLF: %v", err)
	}
	if !d.CRLF {
		t.Error("CRLF file was not detected as such")
	}
	if d.Content != "one\ntwo\n" {
		t.Fatalf("CRLF was not normalised: %q", d.Content)
	}
	if _, err := files.WriteFile("test-server", dos, d.Content, d.ModTime, d.CRLF); err != nil {
		t.Fatalf("write CRLF back: %v", err)
	}
	if err := runRemote(t, pool, `test "$(od -c `+dos+` | grep -c '\\r')" -gt 0`); err != nil {
		t.Errorf("CRLF line endings were not restored on save: %v", err)
	}

	// Binaries and directories are refused before anything is opened.
	if err := runRemote(t, pool, "printf 'ELF\\000\\001\\002' > "+dir+"/blob.bin"); err != nil {
		t.Fatal(err)
	}
	if _, err := files.ReadFile("test-server", dir+"/blob.bin"); err == nil {
		t.Error("expected a binary file to be refused")
	} else {
		t.Logf("binary refused: %v", err)
	}
	if _, err := files.ReadFile("test-server", dir); err == nil {
		t.Error("expected a directory to be refused")
	}
}

func runRemote(t *testing.T, pool *sshx.Pool, cmd string) error {
	t.Helper()
	res, err := pool.Exec("test-server", cmd)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("exit %d: %s", res.Code, strings.TrimSpace(res.Stderr))
	}
	return nil
}
