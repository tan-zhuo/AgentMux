// Command agentmux is a desktop control plane for multi-server AI agents.
//
// Everything long-lived runs inside remote tmux sessions; this process is only
// a viewer and controller, so closing it — or losing the network — never stops
// an agent.
package main

import (
	"embed"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"agentmux/internal/app"
	"agentmux/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// The window and taskbar icon. Wails looks for icon resource 3 in the
// executable first and falls back to this, so the app is correctly badged even
// when the binary was built without an embedded resource.
//
//go:embed build/appicon/icon.png
var appIcon []byte

func main() {
	core, err := app.NewCore()
	if err != nil {
		fatal("AgentMux could not start", err)
	}

	agentSvc := app.NewAgentService(core)

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
			application.NewService(agentSvc),
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

// fatal records a startup failure somewhere a user can find it.
//
// Release builds are linked as GUI subsystem binaries so that launching the app
// does not open a console window. The cost is that anything written to stderr
// goes nowhere, which would turn a failure to start into the app simply not
// appearing. Writing the reason next to the database makes it diagnosable.
func fatal(context string, err error) {
	msg := fmt.Sprintf("%s\n\n%s: %v\n", time.Now().Format(time.RFC3339), context, err)
	if dir, derr := store.AppDir(); derr == nil {
		path := filepath.Join(dir, "startup-error.log")
		if f, ferr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); ferr == nil {
			_, _ = f.WriteString(msg)
			_ = f.Close()
			log.Printf("%s: %v (also written to %s)", context, err, path)
			os.Exit(1)
		}
	}
	log.Fatalf("%s: %v", context, err)
}
