// The desktop half of the entrypoint: everything that needs Wails and a native
// webview. The headless server build compiles without this file — and with it,
// without GTK, WebKit or any display at all.
//go:build !headless

package main

import (
	_ "embed"
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

// runApp opens the native window and runs until it closes.
func runApp() {
	core, err := app.NewCore()
	if err != nil {
		fatal("AgentMux could not start", err)
	}

	agentSvc := app.NewAgentService(core)
	updateSvc := app.NewUpdateService(core)

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
			application.NewService(app.NewDesktopService(core)),
			application.NewService(agentSvc),
			application.NewService(updateSvc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
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

	// The native frame cannot be re-coloured after creation, so it is built from
	// the theme the user last chose.
	chrome := core.WindowChrome()
	winTheme := application.Dark
	if chrome.Light {
		winTheme = application.Light
	}

	// Frameless, because the app draws its own macOS-style title bar. Windows
	// frameless decorations are deliberately left enabled: they are what keep the
	// native drop shadow, the Windows 11 rounded corners and the resize borders.
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "AgentMux",
		Width:            1520,
		Height:           940,
		MinWidth:         1024,
		MinHeight:        620,
		Frameless:        true,
		BackgroundColour: chrome.Background,
		Windows: application.WindowsWindow{
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
		},
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 38,
		},
	})

	if err := wailsApp.Run(); err != nil {
		fatal("AgentMux exited with an error", err)
	}
}
