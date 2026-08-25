// Package update keeps this installation current: it asks GitHub for the
// latest release, downloads the build for this platform, verifies it, and
// swaps it in for the executable that is running right now.
//
// Everything here is deliberately boring: one JSON endpoint, one asset, one
// checksum, one rename. The moment an updater gets clever is the moment it
// bricks somebody's install.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mattn/go-ieproxy"
)

// repo is where releases live. The API shape is GitHub's "latest release".
const repo = "tan-zhuo/AgentMux"

// DefaultAPI is the endpoint queried for releases. Tests point this at a
// local server.
const DefaultAPI = "https://api.github.com"

// Release is one published version, reduced to what updating needs.
type Release struct {
	Tag         string // "v0.2.0"
	Notes       string // release body, markdown
	PublishedAt string
	PageURL     string // human-readable release page
	AssetName   string
	AssetURL    string
	AssetSize   int64
	SumURL      string // the asset's .sha256 companion; empty when absent
}

// releaseJSON is the slice of GitHub's response worth reading.
type releaseJSON struct {
	TagName     string `json:"tag_name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	Assets      []struct {
		Name string `json:"name"`
		Size int64  `json:"size"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Client builds an HTTP client that reaches the network the way the OS is
// configured to: the system proxy on Windows, environment proxies elsewhere.
// Go's default transport reads only the environment, which on a desktop with
// a system-wide proxy set in the OS means silently bypassing it — and for
// many users behind such a proxy, a direct connection to the release feed
// goes nowhere at all.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{Proxy: ieproxy.GetProxyFunc()},
	}
}

// Mirrored prefixes a URL with a user-chosen acceleration mirror — the
// "https://mirror.example/https://github.com/…" convention GitHub proxies
// use. Empty mirror means the URL as published.
func Mirrored(mirror, url string) string {
	mirror = strings.TrimSpace(mirror)
	if mirror == "" {
		return url
	}
	return strings.TrimSuffix(mirror, "/") + "/" + url
}

// Latest asks the release feed for the newest published version and picks the
// asset built for this platform. apiBase is DefaultAPI outside of tests;
// mirror, when set, is a proxy prefix for networks that cannot reach GitHub.
func Latest(ctx context.Context, client *http.Client, apiBase, mirror string) (Release, error) {
	if apiBase == "" {
		apiBase = DefaultAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		Mirrored(mirror, apiBase+"/repos/"+repo+"/releases/latest"), nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AgentMux-updater")

	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return Release{}, fmt.Errorf("release feed answered %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var rj releaseJSON
	if err := json.NewDecoder(resp.Body).Decode(&rj); err != nil {
		return Release{}, fmt.Errorf("release feed sent something unreadable: %w", err)
	}

	rel := Release{
		Tag:         rj.TagName,
		Notes:       rj.Body,
		PublishedAt: rj.PublishedAt,
		PageURL:     rj.HTMLURL,
	}
	want := AssetName(runtime.GOOS, runtime.GOARCH)
	for _, a := range rj.Assets {
		switch a.Name {
		case want:
			rel.AssetName, rel.AssetURL, rel.AssetSize = a.Name, a.URL, a.Size
		case want + ".sha256":
			rel.SumURL = a.URL
		}
	}
	// A release with no build for this platform is still a release. Knowing a
	// newer version exists is useful on every build — the phone, an ARM server
	// running the headless binary — while only installing one needs the asset,
	// and the installer says so itself. Failing the check here is how "check
	// for updates" came to report an error on platforms that update by hand.
	return rel, nil
}

// AssetName is the file the release workflow publishes for a platform.
//
// Not every platform has one: the desktop archives cover amd64 Linux, Windows
// and universal macOS, the phone has its APK, and anything else — an arm64
// Linux desktop, a *BSD build — comes back with a name the release does not
// carry, which Latest reports as a release without an asset.
func AssetName(goos, goarch string) string {
	switch goos {
	case "darwin":
		return "agentmux-macos-universal.zip"
	case "windows":
		return "agentmux-windows-" + goarch + ".zip"
	case "android":
		// The phone carries the same core inside its APK, which is what a
		// newer version arrives as — installed by Android, not by us.
		return "agentmux-android.apk"
	default:
		return "agentmux-linux-" + goarch + ".tar.gz"
	}
}

// Newer reports whether latest is a strictly newer version than current.
//
// A "dev" build has no place in an ordering, so nothing is ever newer than
// it — a developer's working copy must not be overwritten by an updater.
// Pre-release suffixes are compared as plain strings after the numbers, which
// gets v1.2.3-beta.2 vs -beta.10 wrong; releases here do not use them.
func Newer(current, latest string) bool {
	if strings.TrimSpace(latest) == "" || current == "dev" {
		return false
	}
	cur, okC := parseVersion(current)
	lat, okL := parseVersion(latest)
	if !okL {
		return false
	}
	if !okC {
		// An unparseable current version (a hand-built binary, a mangled
		// ldflag) still deserves updates.
		return true
	}
	for i := 0; i < 3; i++ {
		if lat.nums[i] != cur.nums[i] {
			return lat.nums[i] > cur.nums[i]
		}
	}
	// Same numbers: a release beats a pre-release; two pre-releases compare
	// as strings, which is as much order as they get.
	if cur.pre == lat.pre {
		return false
	}
	if lat.pre == "" {
		return true
	}
	if cur.pre == "" {
		return false
	}
	return lat.pre > cur.pre
}

type version struct {
	nums [3]int
	pre  string
}

func parseVersion(s string) (version, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	var v version
	if s == "" {
		return v, false
	}
	if at := strings.IndexAny(s, "-+"); at >= 0 {
		v.pre = s[at+1:]
		s = s[:at]
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v.nums[i] = n
	}
	return v, true
}
