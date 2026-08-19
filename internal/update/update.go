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

// Latest asks the release feed for the newest published version and picks the
// asset built for this platform. apiBase is DefaultAPI outside of tests.
func Latest(ctx context.Context, client *http.Client, apiBase string) (Release, error) {
	if apiBase == "" {
		apiBase = DefaultAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		apiBase+"/repos/"+repo+"/releases/latest", nil)
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
	if rel.AssetURL == "" {
		return rel, fmt.Errorf("release %s has no build for %s/%s", rj.TagName, runtime.GOOS, runtime.GOARCH)
	}
	return rel, nil
}

// AssetName is the file the release workflow publishes for a platform.
func AssetName(goos, goarch string) string {
	switch goos {
	case "darwin":
		return "agentmux-macos-universal.zip"
	case "windows":
		return "agentmux-windows-" + goarch + ".zip"
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
