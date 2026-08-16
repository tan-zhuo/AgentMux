package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agentmux/internal/metrics"
	"agentmux/internal/sftpx"
)

func TestMetricsCollect(t *testing.T) {
	pool, _ := newPool(t)

	s := metrics.Collect(pool, "test-server", time.Now().Unix())
	if !s.OK {
		t.Fatalf("collect failed: %s", s.Error)
	}
	if s.Cores <= 0 {
		t.Errorf("expected a positive core count, got %d", s.Cores)
	}
	if s.MemTotalBytes == 0 {
		t.Error("expected a non-zero total memory")
	}
	if s.MemPercent <= 0 || s.MemPercent > 100 {
		t.Errorf("memory percent out of range: %.1f", s.MemPercent)
	}
	if s.UptimeSeconds <= 0 {
		t.Error("expected a positive uptime")
	}
	if s.Processes <= 0 {
		t.Error("expected to count some processes")
	}
	if len(s.Disks) == 0 {
		t.Error("expected at least one real filesystem")
	}
	for _, d := range s.Disks {
		if d.TotalBytes == 0 || d.Mount == "" {
			t.Errorf("bad disk row: %+v", d)
		}
	}
	// CPU busy is a rate, so it must be inside the valid range even on an idle
	// box; a parsing slip typically shows up as a wild number here.
	if s.CPUPercent < 0 || s.CPUPercent > 100 {
		t.Errorf("cpu percent out of range: %.2f", s.CPUPercent)
	}

	// The richer fields must actually be populated, not silently zero: a broken
	// parser looks identical to an idle machine unless something is asserted.
	if len(s.PerCore) != s.Cores {
		t.Errorf("per-core readings: got %d for %d cores", len(s.PerCore), s.Cores)
	}
	if s.Kernel == "" || s.Arch == "" {
		t.Errorf("expected kernel and arch, got %q / %q", s.Kernel, s.Arch)
	}
	if s.OpenFDs <= 0 {
		t.Error("expected some open file descriptors")
	}
	if s.ContextRate <= 0 {
		t.Error("expected a non-zero context switch rate on a live host")
	}
	if len(s.TopCPU) == 0 || len(s.TopMem) == 0 {
		t.Error("expected top process tables to be populated")
	}
	for _, p := range s.TopCPU {
		if p.PID <= 0 || p.Command == "" {
			t.Errorf("bad process row: %+v", p)
		}
	}

	t.Logf("%s | %s %s | host %s", s.Distro, s.Kernel, s.Arch, s.Hostname)
	t.Logf("cores=%d cpu=%.1f%% (user %.1f sys %.1f iowait %.1f steal %.1f) load=%.2f (%.2f/core)",
		s.Cores, s.CPUPercent, s.CPUUser, s.CPUSystem, s.CPUIOWait, s.CPUSteal, s.Load1, s.LoadPerCore)
	t.Logf("mem=%s/%s (%.1f%%) cached=%s | ctxt=%.0f/s run=%d blocked=%d",
		metrics.FormatBytes(s.MemUsedBytes), metrics.FormatBytes(s.MemTotalBytes), s.MemPercent,
		metrics.FormatBytes(s.MemCachedBytes), s.ContextRate, s.ProcsRunning, s.ProcsBlocked)
	t.Logf("up=%ds procs=%d users=%d conns=%d fds=%d/%d temp=%.1fC",
		s.UptimeSeconds, s.Processes, s.Users, s.Connections, s.OpenFDs, s.MaxFDs, s.TempC)
	t.Logf("percore=%v", s.PerCore)
	for _, d := range s.Disks {
		t.Logf("  disk %-22s %-8s %5.1f%% of %-9s inodes %.0f%%",
			d.Mount, d.Type, d.UsePercent, metrics.FormatBytes(d.TotalBytes), d.InodePct)
	}
	for _, io := range s.BlockIO {
		t.Logf("  io   %-10s read %s/s write %s/s", io.Name,
			metrics.FormatBytes(uint64(io.ReadPS)), metrics.FormatBytes(uint64(io.WritePS)))
	}
	for _, p := range s.TopCPU {
		t.Logf("  cpu  %5.1f%% %5.1f%% %-6d %-10s %s", p.CPU, p.Mem, p.PID, p.User, p.Command)
	}
	for _, p := range s.TopMem {
		t.Logf("  mem  %5.1f%% %5.1f%% %-6d %-10s %s", p.CPU, p.Mem, p.PID, p.User, p.Command)
	}
}

func TestMetricsCPUReflectsLoad(t *testing.T) {
	pool, _ := newPool(t)

	idle := metrics.Collect(pool, "test-server", time.Now().Unix())
	if !idle.OK {
		t.Fatalf("idle sample failed: %s", idle.Error)
	}

	// Spin one core in the background, then sample again. The reading has to
	// move, otherwise the delta maths is wrong and the panel would be decorative.
	go func() {
		_, _ = pool.Exec("test-server", `timeout 3 sh -c 'while :; do :; done' >/dev/null 2>&1; true`)
	}()
	time.Sleep(900 * time.Millisecond)
	busy := metrics.Collect(pool, "test-server", time.Now().Unix())
	if !busy.OK {
		t.Fatalf("busy sample failed: %s", busy.Error)
	}

	t.Logf("idle=%.1f%% busy=%.1f%% (cores=%d)", idle.CPUPercent, busy.CPUPercent, busy.Cores)
	if busy.CPUPercent <= idle.CPUPercent {
		t.Errorf("expected CPU to rise under load: idle %.2f%% -> busy %.2f%%",
			idle.CPUPercent, busy.CPUPercent)
	}
}

func TestSFTPBrowseAndTransfer(t *testing.T) {
	pool, _ := newPool(t)
	files := sftpx.New(pool, func(string, any) {})
	t.Cleanup(files.Close)

	home, err := files.Home("test-server")
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	t.Logf("home = %s", home)

	remoteDir := home + "/agentmux-sftp-test"
	_ = files.Remove("test-server", remoteDir, true)
	if err := files.Mkdir("test-server", remoteDir); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = files.Remove("test-server", remoteDir, true) })

	// The new directory must show up in a listing of its parent.
	listing, err := files.List("test-server", home)
	if err != nil {
		t.Fatalf("list home: %v", err)
	}
	var found bool
	for _, e := range listing.Entries {
		if e.Name == "agentmux-sftp-test" {
			found = true
			if !e.IsDir {
				t.Error("expected the new entry to be a directory")
			}
		}
	}
	if !found {
		t.Fatalf("new directory missing from listing of %s", home)
	}
	if listing.Parent == "" {
		t.Error("expected a parent path for navigating up")
	}

	// Upload a file with content we can verify byte for byte.
	local := filepath.Join(t.TempDir(), "payload.txt")
	body := strings.Repeat("agentmux sftp round trip\n", 4000) // ~100 KB
	if err := os.WriteFile(local, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	up, err := files.Upload("test-server", local, remoteDir)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	waitTransfer(t, files, up.ID)

	entries, err := files.List("test-server", remoteDir)
	if err != nil {
		t.Fatalf("list uploaded dir: %v", err)
	}
	if len(entries.Entries) != 1 || entries.Entries[0].Name != "payload.txt" {
		t.Fatalf("expected exactly the uploaded file, got %+v", entries.Entries)
	}
	if got, want := entries.Entries[0].Size, int64(len(body)); got != want {
		t.Errorf("uploaded size %d, want %d", got, want)
	}

	// Download it back and compare.
	back := filepath.Join(t.TempDir(), "roundtrip.txt")
	down, err := files.Download("test-server", remoteDir+"/payload.txt", back)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	waitTransfer(t, files, down.ID)

	got, err := os.ReadFile(back)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != body {
		t.Fatalf("round trip changed the file: got %d bytes, want %d", len(got), len(body))
	}
	t.Logf("round tripped %d bytes intact", len(got))

	// Rename, then delete.
	if err := files.Rename("test-server", remoteDir+"/payload.txt", remoteDir+"/renamed.txt"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	after, err := files.List("test-server", remoteDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != 1 || after.Entries[0].Name != "renamed.txt" {
		t.Fatalf("rename did not take: %+v", after.Entries)
	}
	if err := files.Remove("test-server", remoteDir+"/renamed.txt", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
	empty, err := files.List("test-server", remoteDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Entries) != 0 {
		t.Errorf("expected an empty directory after delete, got %+v", empty.Entries)
	}
}

func waitTransfer(t *testing.T, files *sftpx.Client, id string) {
	t.Helper()
	for i := 0; i < 100; i++ {
		for _, tr := range files.Transfers() {
			if tr.ID != id {
				continue
			}
			switch tr.Status {
			case "done":
				if tr.Done != tr.Size {
					t.Errorf("transfer reported %d of %d bytes", tr.Done, tr.Size)
				}
				return
			case "error", "cancelled":
				t.Fatalf("transfer %s: %s %s", id, tr.Status, tr.Error)
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("transfer %s never finished", id)
}
