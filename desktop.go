// The desktop half of the entrypoint: everything that needs Wails and a native
// webview. The headless server build compiles without this file — and with it,
// without GTK, WebKit or any display at all.
//go:build !headless

package main

import (
	_ "embed"
	"fmt"
	"log"
	"net/url"
	"time"

	"agentmux/internal/app"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The window and taskbar icon. Wails looks for icon resource 3 in the
// executable first and falls back to this, so the app is correctly badged even
// when the binary was built without an embedded resource.
//
//go:embed build/appicon/icon.png
var appIcon []byte

// serveUpdateExtras adds nothing when the desktop binary runs --serve: its
// update service replaces the desktop build and relaunches a window, neither
// of which belongs on a server. Updates stay check-only there.
func serveUpdateExtras(*app.Core) []any { return nil }

// runApp opens the native window and runs until it closes.
func runApp() {
	core, err := app.NewCore()
	if err != nil {
		fatal("AgentMux could not start", err)
	}

	agentSvc := app.NewAgentService(core)
	updateSvc := app.NewUpdateService(core)
	desktopSvc := app.NewDesktopService(core)
	connectSvc := app.NewConnectService(core)
	// The webview's page has no origin a WebSocket can be relative to, so the
	// in-app viewer is given a listener on this machine's loopback to talk to.
	// A failure here costs the in-app viewer and nothing else: the system
	// client is opened through a different path entirely.
	if err := desktopSvc.EnableLoopback(); err != nil {
		log.Printf("in-app desktop sessions are unavailable: %v", err)
	}

	wailsApp := application.New(application.Options{
		Name:        "AgentMux",
		Description: "Multi-server AI agent and SSH cluster control plane",
		Icon:        appIcon,
		Services: []application.Service{
			application.NewService(app.NewServerService(core)),
			application.NewService(app.NewTreeService(core)),
			application.NewService(app.NewTerminalService(core)),
			application.NewService(app.NewTmuxService(core)),
			application.NewService(app.NewToolkitService(core)),
			application.NewService(app.NewFileService(core)),
			application.NewService(app.NewMetricsService(core)),
			application.NewService(app.NewWindowService(core)),
			application.NewService(app.NewLLMService(core)),
			application.NewService(app.NewMemoryService(core)),
			application.NewService(app.NewSkillService(core)),
			application.NewService(app.NewOrchService(core)),
			application.NewService(app.NewConfigService(core)),
			application.NewService(desktopSvc),
			application.NewService(agentSvc),
			application.NewService(updateSvc),
			application.NewService(connectSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			// macOS keeps an application alive after its last window closes, on
			// the understanding that clicking the Dock icon brings a window
			// back. Wails has no reopen handler, so there is nothing to bring
			// back: the process sits there with no window, the Dock icon
			// activates an app that cannot present one, and the only way out is
			// Force Quit. AgentMux has one window and closing it is the user
			// saying they are done — the work itself lives in tmux on the
			// servers and is not affected either way. When modes switch, the
			// new window is created before the old one closes, so the count
			// never touches zero.
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		OnShutdown: func() { core.Shutdown() },
	})

	core.SetEmitter(func(name string, data any) {
		wailsApp.Event.Emit(name, data)
	})
	core.StartPoller(agentSvc, 5*time.Second)
	// A quiet daily rhythm plus a check at launch; anyone impatient can ask
	// from the settings dialog.
	updateSvc.StartWatch(6 * time.Hour)

	// openMain shows the app's own UI; openRemote points a window at a remote
	// `agentmux --serve` instead. Each closes the other's window after opening
	// its own, so a switch is a swap and never leaves zero windows.
	openMain := func() {
		openMainWindow(wailsApp, core)
		if w, ok := wailsApp.Window.GetByName("remote"); ok {
			w.Close()
		}
	}
	openRemote := func(pageURL, addr string) {
		openRemoteWindow(wailsApp, core, pageURL, addr, connectSvc.ControlURL())
		if w, ok := wailsApp.Window.GetByName("main"); ok {
			w.Close()
		}
	}
	connectSvc.SetOpeners(openMain, openRemote)
	if err := connectSvc.StartControl(); err != nil {
		log.Printf("connect control endpoint unavailable: %v", err)
	}

	// A remembered remote is only honoured when the way back — the loopback
	// control endpoint — is standing; otherwise the local UI, which can
	// always switch again, is the recoverable place to be. Nothing here waits
	// on the network: a server that is down shows as the proxy's error page,
	// which explains itself and carries its own way home.
	if pageURL, addr, ok := connectSvc.StartupRemote(); ok && connectSvc.ControlURL() != "" {
		openRemote(pageURL, addr)
	} else {
		openMain()
	}

	if err := wailsApp.Run(); err != nil {
		fatal("AgentMux exited with an error", err)
	}
}

// windowsChrome builds the themed Windows border settings both windows share.
func windowsChrome(core *app.Core) (application.WindowsWindow, application.RGBA) {
	chrome := core.WindowChrome()
	winTheme := application.Dark
	if chrome.Light {
		winTheme = application.Light
	}
	return application.WindowsWindow{
		Theme: winTheme,
		// The window is frameless so there is no title bar to draw an icon
		// in, but the icon is what the taskbar and Alt+Tab use.
		DisableIcon: false,
		// Windows 11 draws a light system border around the window, which
		// reads as a bright hairline against a dark UI. Match it to the theme.
		CustomTheme: application.ThemeSettings{
			DarkModeActive:    &application.WindowTheme{BorderColour: chrome.Border},
			DarkModeInactive:  &application.WindowTheme{BorderColour: chrome.BorderInactive},
			LightModeActive:   &application.WindowTheme{BorderColour: chrome.Border},
			LightModeInactive: &application.WindowTheme{BorderColour: chrome.BorderInactive},
		},
	}, chrome.Background
}

// openMainWindow opens the frameless window on the app's own frontend.
//
// The native frame cannot be re-coloured after creation, so it is built from
// the theme the user last chose. Frameless, because the app draws its own
// macOS-style title bar. Windows frameless decorations are deliberately left
// enabled: they are what keep the native drop shadow, the Windows 11 rounded
// corners and the resize borders.
func openMainWindow(wailsApp *application.App, core *app.Core) {
	win, background := windowsChrome(core)
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "AgentMux",
		Width:            1520,
		Height:           940,
		MinWidth:         1024,
		MinHeight:        620,
		Frameless:        true,
		BackgroundColour: background,
		Windows:          win,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 38,
		},
	})
}

// openRemoteWindow points a window at a remote `agentmux --serve` — at the
// address itself, or at the loopback proxy that holds a pinned certificate.
//
// The window keeps its native frame: the page comes from the server and talks
// only to the server, so the frameless window's webview-drawn controls would
// have no Go on their origin to answer them. The control URL rides along in
// the hash — it is how that page, which cannot reach this process over the
// Wails bridge, asks to be switched back.
func openRemoteWindow(wailsApp *application.App, core *app.Core, pageURL, addr, controlURL string) {
	// The query nonce makes every switch a real navigation: two remotes share
	// the loopback proxy's origin, and a URL that differs only in its hash is
	// a same-document navigation — the old server's page would keep running
	// against a proxy now pointed somewhere else. (The page scrubs the query
	// with the hash right after claiming it, so the address stays clean.)
	target := fmt.Sprintf("%s/?s=%x", pageURL, time.Now().UnixNano())
	if controlURL != "" {
		// The real address rides along too: a page reached through the
		// loopback proxy would otherwise only know the proxy as its origin,
		// and Settings would show — and compare against — the wrong thing.
		target += "#back=" + url.QueryEscape(controlURL) + "&raddr=" + url.QueryEscape(addr)
	}
	if w, ok := wailsApp.Window.GetByName("remote"); ok {
		w.SetURL(target)
		w.Focus()
		return
	}
	win, background := windowsChrome(core)
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "remote",
		Title:            "AgentMux",
		URL:              target,
		Width:            1520,
		Height:           940,
		MinWidth:         1024,
		MinHeight:        620,
		BackgroundColour: background,
		Windows:          win,
	})
}
