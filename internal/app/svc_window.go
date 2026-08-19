// Window management rides on the Wails webview, which the headless server
// build neither has nor links — GTK and WebKit stay off a server's shoulders.
//go:build !headless

package app

import (
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"agentmux/internal/store"
)

// DetachedTab is the handover payload for a tab torn out into its own window.
//
// It travels through a token rather than the URL: the window is opened with
// "#d=<token>" and the new page claims the payload, which keeps paths and
// commands out of a URL that would have to be escaped and unescaped correctly.
type DetachedTab struct {
	Token       string `json:"token"`
	Title       string `json:"title"`
	Kind        string `json:"kind"`
	ServerID    string `json:"serverId"`
	WorkspaceID string `json:"workspaceId"`
	AgentID     string `json:"agentId"`
	TmuxSession string `json:"tmuxSession"`
	Command     string `json:"command"`
	// ShellID adopts a PTY that is already open, so tearing a tab out of the
	// main window keeps the same session rather than starting a second one.
	ShellID string `json:"shellId"`
}

// WindowService opens and manages extra windows.
type WindowService struct {
	core *Core

	mu      sync.Mutex
	pending map[string]DetachedTab
}

// NewWindowService binds a window service to the core.
func NewWindowService(c *Core) *WindowService {
	return &WindowService{core: c, pending: map[string]DetachedTab{}}
}

// ServiceName identifies the service in Wails logs.
func (w *WindowService) ServiceName() string { return "WindowService" }

// Detach opens a new window showing one tab, positioned where it was dropped.
func (w *WindowService) Detach(tab DetachedTab, x, y, width, height int) (string, error) {
	appInstance := application.Get()
	if appInstance == nil {
		return "", errors.New("application is not running")
	}
	if tab.Kind == "" {
		return "", errors.New("tab kind is required")
	}

	tab.Token = uuid.NewString()
	w.mu.Lock()
	w.pending[tab.Token] = tab
	w.mu.Unlock()

	if width < 640 {
		width = 1000
	}
	if height < 400 {
		height = 680
	}

	title := tab.Title
	if title == "" {
		title = "AgentMux"
	}

	chrome := w.core.WindowChrome()
	winTheme := application.Dark
	if chrome.Light {
		winTheme = application.Light
	}

	appInstance.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "detached-" + tab.Token,
		Title:            title,
		URL:              "/#d=" + tab.Token,
		Width:            width,
		Height:           height,
		MinWidth:         520,
		MinHeight:        320,
		X:                x,
		Y:                y,
		Frameless:        true,
		BackgroundColour: chrome.Background,
		Windows: application.WindowsWindow{
			Theme:       winTheme,
			DisableIcon: false,
			CustomTheme: application.ThemeSettings{
				DarkModeActive:    &application.WindowTheme{BorderColour: chrome.Border},
				DarkModeInactive:  &application.WindowTheme{BorderColour: chrome.BorderInactive},
				LightModeActive:   &application.WindowTheme{BorderColour: chrome.Border},
				LightModeInactive: &application.WindowTheme{BorderColour: chrome.BorderInactive},
			},
		},
		Mac: application.MacWindow{InvisibleTitleBarHeight: 38},
	})

	return tab.Token, nil
}

// Claim hands a detached window its payload. The token is single use, so a
// reload cannot resurrect a tab that was already taken.
func (w *WindowService) Claim(token string) (DetachedTab, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	tab, ok := w.pending[token]
	if !ok {
		return DetachedTab{}, fmt.Errorf("this window's tab has already been claimed")
	}
	delete(w.pending, token)
	return tab, nil
}

// FromTerminalTab converts a persisted tab into a detach payload.
func FromTerminalTab(t store.TerminalTab, shellID string) DetachedTab {
	return DetachedTab{
		Title:       t.Title,
		Kind:        t.Kind,
		ServerID:    t.ServerID,
		WorkspaceID: t.WorkspaceID,
		AgentID:     t.AgentID,
		TmuxSession: t.TmuxSession,
		Command:     t.Command,
		ShellID:     shellID,
	}
}
