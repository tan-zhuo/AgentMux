package app

import (
	"context"
	"time"

	"agentmux/internal/update"
)

// SettingUpdateMirror is a user-chosen proxy prefix for release checks and
// downloads — the way through for networks where GitHub is unreachable.
// Empty means direct.
const SettingUpdateMirror = "update.mirror"

// UpdateInfo is what a version check found. Shared between the desktop's
// self-updating service and the check-only service every build carries.
type UpdateInfo struct {
	CurrentVersion string `json:"currentVersion"`
	LatestVersion  string `json:"latestVersion"`
	PublishedAt    string `json:"publishedAt"`
	PageURL        string `json:"pageUrl"`
	Notes          string `json:"notes"`
	AssetSize      int64  `json:"assetSize"`
	HasUpdate      bool   `json:"hasUpdate"`
	// CanApply: whether a served browser may offer the upgrade button — a
	// static property of the build (serveCanApply, build-tagged), carried
	// here so the page needs no second question and no fallible probe.
	CanApply bool   `json:"canApply"`
	Error    string `json:"error"`
}

// fetchLatest asks the release feed for the newest version and reports how it
// compares to this build. Which asset it looks for is the build's own answer
// (releaseAssetName, build-tagged): the desktop archive or the server tarball.
func fetchLatest(mirror string) (update.Release, UpdateInfo) {
	info := UpdateInfo{CurrentVersion: Version, CanApply: serveCanApply()}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	rel, err := update.Latest(ctx, update.Client(20*time.Second), "", mirror, releaseAssetName())
	if err != nil {
		info.Error = err.Error()
		return rel, info
	}

	info.LatestVersion = rel.Tag
	info.PublishedAt = rel.PublishedAt
	info.PageURL = rel.PageURL
	info.Notes = rel.Notes
	info.AssetSize = rel.AssetSize
	info.HasUpdate = update.Newer(Version, rel.Tag)
	return rel, info
}

// UpdateCheckService answers "is there a newer version" and nothing else.
// It exists because knowing about a release is useful everywhere — the phone,
// a served browser, a headless box — while replacing the running binary only
// makes sense for the desktop app, which has its own service for that.
type UpdateCheckService struct{ core *Core }

// NewUpdateCheckService builds the check-only service.
func NewUpdateCheckService(c *Core) *UpdateCheckService { return &UpdateCheckService{core: c} }

// ServiceName identifies the service in logs.
func (s *UpdateCheckService) ServiceName() string { return "UpdateCheckService" }

// Check asks the release feed for the newest version.
func (s *UpdateCheckService) Check() UpdateInfo {
	_, info := fetchLatest(s.core.Store.GetSetting(SettingUpdateMirror, ""))
	return info
}
