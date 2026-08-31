// The serve flavour of self-update: a headless Linux server downloads the
// server tarball, swaps its own binary and execs over itself — the same PID
// carries on under systemd or a wrapper script, and the agents in remote tmux
// never notice. The desktop flavour lives in svc_update.go; the engine they
// share is svc_update_common.go.
//
// This build also runs embedded in the Android app, where the core is updated
// by installing a newer APK — CanApply says no there, and Apply refuses.
//go:build headless

package app

import (
	"log"
	"runtime"
	"time"

	"agentmux/internal/update"
)

// releaseAssetName is which release asset this build updates itself with: the
// static server tarball on Linux, nothing anywhere else (the Android core
// arrives inside the APK; other platforms build by hand).
func releaseAssetName() string {
	if runtime.GOOS == "linux" {
		return update.ServerAssetName(runtime.GOARCH)
	}
	return ""
}

// UpdateService keeps a headless server current. It checks the release feed
// in the background, announces newer versions over the event stream, and on
// request downloads the server build, swaps it in and restarts in place.
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

// ServiceName identifies the service in logs.
func (u *UpdateService) ServiceName() string { return "UpdateService" }

// CanApply reports whether this build can replace itself — the frontend asks
// before offering an upgrade button to a served browser.
func (u *UpdateService) CanApply() bool { return releaseAssetName() != "" }

// Check asks the release feed for the newest version.
func (u *UpdateService) Check() UpdateInfo { return u.check() }

// StartWatch checks shortly after launch and then on an interval, so every
// connected browser hears about new versions without asking.
func (u *UpdateService) StartWatch(interval time.Duration) { u.startWatch(interval) }

// Apply downloads the latest server build, verifies it, swaps it in and execs
// the new binary in place. Progress goes out as events; the call returns just
// before the restart so the UI gets the answer. Browsers ride out the restart
// the way they ride out any dropped connection: the event stream reconnects.
func (u *UpdateService) Apply() UpdateResult {
	if !u.CanApply() {
		return UpdateResult{Error: "this build does not update itself — install the newer version the way this one was installed"}
	}
	return u.apply(func(restartPath string) {
		// A clean shutdown first: SSH connections and the database close the
		// way they do on exit. The work lives in remote tmux either way.
		u.core.Shutdown()
		if err := update.Restart(restartPath); err != nil {
			// The new build is installed but could not take over; the next
			// start of this service — by hand or by a supervisor — runs it.
			log.Printf("installed %s but could not restart into it: %v", restartPath, err)
		}
	})
}
