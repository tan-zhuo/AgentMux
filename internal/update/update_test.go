package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.2.0", false},
		{"v0.3.0", "v0.2.9", false},
		{"v0.2.0", "v0.2.1", true},
		{"v1.9.0", "v1.10.0", true},
		{"0.1.0", "v0.2.0", true},   // missing v prefix still compares
		{"dev", "v99.0.0", false},   // a dev build is never overwritten
		{"v0.1.0", "", false},       // an empty feed answer is not an update
		{"garbage", "v0.1.0", true}, // unparseable current still gets updates
		{"v0.2.0", "garbage", false},
		{"v1.0.0-rc.1", "v1.0.0", true}, // release beats its own pre-release
		{"v1.0.0", "v1.0.0-rc.1", false},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestLatestPicksThisPlatformsAsset(t *testing.T) {
	name := AssetName(runtime.GOOS, runtime.GOARCH)
	feed := fmt.Sprintf(`{
		"tag_name": "v0.9.0",
		"body": "notes",
		"html_url": "https://example.test/rel",
		"published_at": "2026-08-19T00:00:00Z",
		"assets": [
			{"name": "unrelated.txt", "size": 1, "browser_download_url": "https://example.test/x"},
			{"name": %q, "size": 1234, "browser_download_url": "https://example.test/a"},
			{"name": %q, "size": 100, "browser_download_url": "https://example.test/a.sha256"}
		]
	}`, name, name+".sha256")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/"+repo+"/releases/latest" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, feed)
	}))
	defer srv.Close()

	rel, err := Latest(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != "v0.9.0" || rel.AssetName != name || rel.AssetSize != 1234 ||
		rel.AssetURL != "https://example.test/a" || rel.SumURL != "https://example.test/a.sha256" {
		t.Errorf("release = %+v", rel)
	}
}

func TestLatestRejectsMissingAsset(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"tag_name": "v0.9.0", "assets": []}`)
	}))
	defer srv.Close()
	if _, err := Latest(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("expected an error for a release with no asset for this platform")
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	payload := []byte("the new build")
	sum := sha256.Sum256(payload)
	mux := http.NewServeMux()
	mux.HandleFunc("/asset", func(w http.ResponseWriter, r *http.Request) { w.Write(payload) })
	mux.HandleFunc("/asset.sha256", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  agentmux.tar.gz\n", hex.EncodeToString(sum[:]))
	})
	mux.HandleFunc("/bad.sha256", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%064d  agentmux.tar.gz\n", 0)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel := Release{AssetName: "asset.bin", AssetURL: srv.URL + "/asset", SumURL: srv.URL + "/asset.sha256"}
	var calls int
	path, err := Download(context.Background(), srv.Client(), rel, t.TempDir(),
		func(done, total int64) { calls++ })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(payload) || calls == 0 {
		t.Errorf("downloaded %q with %d progress calls", got, calls)
	}

	rel.SumURL = srv.URL + "/bad.sha256"
	if _, err := Download(context.Background(), srv.Client(), rel, t.TempDir(), nil); err == nil {
		t.Fatal("a wrong checksum must fail the download")
	}
}

func TestExtractTarGzFile(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "a.tar.gz")
	f, _ := os.Create(archive)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{
		"agentmux-linux-amd64/README.md": "docs",
		"agentmux-linux-amd64/agentmux":  "ELF!",
	} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	f.Close()

	dest := filepath.Join(t.TempDir(), "bin")
	if err := extractTarGzFile(archive, "/agentmux", dest); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "ELF!" {
		t.Errorf("extracted %q", got)
	}
}

func TestExtractZipFile(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "a.zip")
	f, _ := os.Create(archive)
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"agentmux-windows-amd64/LICENSE":      "mit",
		"agentmux-windows-amd64/agentmux.exe": "MZ!",
	} {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()

	dest := filepath.Join(t.TempDir(), "bin")
	if err := extractZipFile(archive, "/agentmux.exe", dest); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != "MZ!" {
		t.Errorf("extracted %q", got)
	}
}

func TestMoveFileFallsBackToCopy(t *testing.T) {
	src := filepath.Join(t.TempDir(), "src")
	dst := filepath.Join(t.TempDir(), "dst")
	os.WriteFile(src, []byte("bits"), 0o755)
	if err := moveFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "bits" {
		t.Errorf("moved %q", got)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source survived the move")
	}
}

func TestBundleRoot(t *testing.T) {
	if got := bundleRoot("/Applications/AgentMux.app/Contents/MacOS/AgentMux"); got != "/Applications/AgentMux.app" {
		t.Errorf("bundleRoot = %q", got)
	}
	if got := bundleRoot("/usr/local/bin/agentmux"); got != "" {
		t.Errorf("bundleRoot on a bare binary = %q", got)
	}
}
