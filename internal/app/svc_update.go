// The desktop flavour of self-update: it replaces the desktop binary (or .app
// bundle), relaunches it and quits this window. The serve build has its own
// flavour in svc_update_serve.go; the engine they share is svc_update_common.go.
//go:build !headless

package app

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"agentmux/internal/update"
)

// releaseAssetName is which release asset this build updates itself with:
// the desktop archive for the platform, which is Latest's default.
func releaseAssetName() string { return "" }

// UpdateService keeps this installation current. It checks the release feed
// in the background, tells the UI when a newer version exists, and on request
// downloads it, swaps it in and restarts.
type UpdateService struct {
	updateRunner
}

// NewUpdateService binds an update service to the core and sweeps up whatever
// a previous upgrade left behind.
func NewUpdateService(c *Core) *UpdateService {
	u := &UpdateService{updateRunner{core: c}}
	go update.CleanupOld(u.stagingDir())
	return u
}

// ServiceName identifies the service in Wails logs.
func (u *UpdateService) ServiceName() string { return "UpdateService" }

// Check asks the release feed for the newest version.
func (u *UpdateService) Check() UpdateInfo { return u.check() }

// StartWatch checks shortly after launch and then on an interval, so a user
// who never opens the settings still hears about new versions.
func (u *UpdateService) StartWatch(interval time.Duration) { u.startWatch(interval) }

// Apply downloads the latest release, verifies it, swaps it in for the
// running build and restarts the app. Progress goes out as events; the call
// returns just before the restart so the UI gets the answer.
func (u *UpdateService) Apply() UpdateResult {
	return u.apply(func(restartPath string) {
		_ = update.Relaunch(restartPath)
		application.Get().Quit()
	})
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
