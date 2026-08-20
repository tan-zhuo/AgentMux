// Self-update replaces the desktop binary and restarts the window; a headless
// server is updated by whoever runs it, so the whole service stays out.
//go:build !headless

package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"agentmux/internal/update"
)

// UpdateService keeps this installation current. It checks the release feed
// in the background, tells the UI when a newer version exists, and on request
// downloads it, swaps it in and restarts.
type UpdateService struct {
	core *Core

	mu     sync.Mutex
	busy   bool
	latest *update.Release
}

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

// NewUpdateService binds an update service to the core and sweeps up whatever
// a previous upgrade left behind.
func NewUpdateService(c *Core) *UpdateService {
	u := &UpdateService{core: c}
	go update.CleanupOld(u.stagingDir())
	return u
}

// ServiceName identifies the service in Wails logs.
func (u *UpdateService) ServiceName() string { return "UpdateService" }

func (u *UpdateService) stagingDir() string {
	return filepath.Join(u.core.Store.Dir, "update-staging")
}

// Check asks the release feed for the newest version. Any check that finds
// one also announces it, so the banner appears no matter who asked.
func (u *UpdateService) Check() UpdateInfo {
	rel, info := fetchLatest()
	if info.Error != "" {
		return info
	}

	u.mu.Lock()
	u.latest = &rel
	u.mu.Unlock()

	if info.HasUpdate {
		u.core.Emit(eventUpdateAvailable, info)
	}
	return info
}

// StartWatch checks shortly after launch and then on an interval, so a user
// who never opens the settings still hears about new versions.
func (u *UpdateService) StartWatch(interval time.Duration) {
	go func() {
		time.Sleep(10 * time.Second)
		for {
			u.Check()
			time.Sleep(interval)
		}
	}()
}

// Apply downloads the latest release, verifies it, swaps it in for the
// running build and restarts the app. Progress goes out as events; the call
// returns just before the restart so the UI gets the answer.
func (u *UpdateService) Apply() UpdateResult {
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
		info := u.Check()
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

	staging := u.stagingDir()
	os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return UpdateResult{Error: err.Error()}
	}

	// Progress is throttled to whole-percent changes: a 100 MB download at
	// 128 KB reads would otherwise flood the IPC bridge with noise.
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
	archive, err := update.Download(context.Background(), &http.Client{}, *rel, staging, onProgress)
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
		_ = update.Relaunch(restartPath)
		application.Get().Quit()
	}()
	return UpdateResult{OK: true}
}

// OpenReleasePage shows the latest release in the browser — the way out when
// an automatic update cannot run, and the place to read full release notes.
func (u *UpdateService) OpenReleasePage() {
	u.mu.Lock()
	url := ""
	if u.latest != nil {
		url = u.latest.PageURL
	}
	u.mu.Unlock()
	if url == "" {
		url = "https://github.com/tan-zhuo/AgentMux/releases/latest"
	}
	_ = application.Get().Browser.OpenURL(url)
}
