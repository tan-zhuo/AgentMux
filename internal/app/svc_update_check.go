package app

import (
	"context"
	"net/http"
	"time"

	"agentmux/internal/update"
)

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
	Error          string `json:"error"`
}

// fetchLatest asks the release feed for the newest version and reports how it
// compares to this build.
func fetchLatest() (update.Release, UpdateInfo) {
	info := UpdateInfo{CurrentVersion: Version}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 20 * time.Second}
	rel, err := update.Latest(ctx, client, "")
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
type UpdateCheckService struct{}

// NewUpdateCheckService builds the check-only service.
func NewUpdateCheckService() *UpdateCheckService { return &UpdateCheckService{} }

// ServiceName identifies the service in logs.
func (s *UpdateCheckService) ServiceName() string { return "UpdateCheckService" }

// Check asks the release feed for the newest version.
func (s *UpdateCheckService) Check() UpdateInfo {
	_, info := fetchLatest()
	return info
}
