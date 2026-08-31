// The engine both update services share: checking the feed, downloading with
// progress, verifying and swapping the binary. What differs per build is only
// which asset names it (releaseAssetName, build-tagged) and how the new build
// takes over (the restart callback) — the desktop relaunches and quits its
// window, the server execs over itself.
package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"agentmux/internal/update"
)

// Event names the frontend subscribes to.
const (
	eventUpdateAvailable = "update:available"
	eventUpdateProgress  = "update:progress"
)

// UpdateProgress narrates one step of an upgrade to the UI.
type UpdateProgress struct {
	// Phase is download, install or restart.
	Phase      string  `json:"phase"`
	DoneBytes  int64   `json:"doneBytes"`
	TotalBytes int64   `json:"totalBytes"`
	Percent    float64 `json:"percent"`
}

// UpdateResult is Apply's answer. When OK, the app is about to restart itself.
type UpdateResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

// updateRunner holds the state and steps of an upgrade.
type updateRunner struct {
	core *Core

	mu       sync.Mutex
	busy     bool
	latest   *update.Release
	lastInfo UpdateInfo
	lastAt   time.Time
}

func (u *updateRunner) stagingDir() string {
	return filepath.Join(u.core.Store.Dir, "update-staging")
}

func (u *updateRunner) mirror() string {
	return u.core.Store.GetSetting(SettingUpdateMirror, "")
}

// check asks the release feed for the newest version. Any check that finds
// one also announces it, so the banner appears no matter who asked.
//
// A recent answer is served from memory: on a server, every connecting
// browser asks on load, and GitHub's unauthenticated rate limit is not sized
// for a page reload habit. Five minutes is invisible to a person and plenty
// for the limit.
func (u *updateRunner) check() UpdateInfo {
	u.mu.Lock()
	if u.lastInfo.Error == "" && !u.lastAt.IsZero() && time.Since(u.lastAt) < 5*time.Minute {
		info := u.lastInfo
		u.mu.Unlock()
		return info
	}
	u.mu.Unlock()

	rel, info := fetchLatest(u.mirror())
	if info.Error != "" {
		return info
	}

	u.mu.Lock()
	u.latest = &rel
	u.lastInfo = info
	u.lastAt = time.Now()
	u.mu.Unlock()

	if info.HasUpdate {
		u.core.Emit(eventUpdateAvailable, info)
	}
	return info
}

// startWatch checks shortly after launch and then on an interval, so a user
// who never opens the settings still hears about new versions.
func (u *updateRunner) startWatch(interval time.Duration) {
	go func() {
		time.Sleep(10 * time.Second)
		for {
			u.check()
			time.Sleep(interval)
		}
	}()
}

// apply downloads the latest release, verifies it and swaps it in for the
// running build, then hands the path of the new build to restart — off this
// call's back, so the answer reaches the UI before the process goes away.
func (u *updateRunner) apply(restart func(restartPath string)) UpdateResult {
	if Version == "dev" {
		return UpdateResult{Error: "this is a dev build — it updates through git, not the release feed"}
	}

	u.mu.Lock()
	if u.busy {
		u.mu.Unlock()
		return UpdateResult{Error: "an update is already running"}
	}
	u.busy = true
	rel := u.latest
	u.mu.Unlock()
	defer func() {
		u.mu.Lock()
		u.busy = false
		u.mu.Unlock()
	}()

	if rel == nil {
		info := u.check()
		if info.Error != "" {
			return UpdateResult{Error: info.Error}
		}
		u.mu.Lock()
		rel = u.latest
		u.mu.Unlock()
	}
	if rel == nil || !update.Newer(Version, rel.Tag) {
		return UpdateResult{Error: "there is no newer version to install"}
	}
	// The check no longer fails on a release that has no build for this
	// platform — it is still news worth showing — so this is where a platform
	// that updates by hand is told to do that.
	if rel.AssetURL == "" {
		return UpdateResult{Error: fmt.Sprintf(
			"%s has no build for %s/%s — install it from the release page",
			rel.Tag, runtime.GOOS, runtime.GOARCH)}
	}

	staging := u.stagingDir()
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return UpdateResult{Error: err.Error()}
	}

	// Progress is throttled to whole-percent changes: a 100 MB download at
	// 128 KB reads would otherwise flood the transport with noise.
	lastPct := -1
	onProgress := func(done, total int64) {
		pct := 0.0
		if total > 0 {
			pct = float64(done) / float64(total) * 100
		}
		if int(pct) == lastPct && done < total {
			return
		}
		lastPct = int(pct)
		u.core.Emit(eventUpdateProgress, UpdateProgress{
			Phase: "download", DoneBytes: done, TotalBytes: total, Percent: pct,
		})
	}

	// No overall timeout: a slow connection downloading a large build is not
	// an error. Cancellation rides on the app quitting.
	archive, err := update.Download(context.Background(), update.Client(0), *rel, staging, u.mirror(), onProgress)
	if err != nil {
		return UpdateResult{Error: fmt.Sprintf("download failed: %v", err)}
	}

	u.core.Emit(eventUpdateProgress, UpdateProgress{Phase: "install", Percent: 100})
	restartPath, err := update.Apply(archive, staging)
	if err != nil {
		return UpdateResult{Error: err.Error()}
	}

	// The answer has to reach the UI before the process goes away, so the
	// restart happens a beat later, off this call's back.
	u.core.Emit(eventUpdateProgress, UpdateProgress{Phase: "restart", Percent: 100})
	go func() {
		time.Sleep(600 * time.Millisecond)
		restart(restartPath)
	}()
	return UpdateResult{OK: true}
}
