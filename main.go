// Command agentmux is a desktop control plane for multi-server AI agents.
//
// Everything long-lived runs inside remote tmux sessions; this process is only
// a viewer and controller, so closing it — or losing the network — never stops
// an agent.
package main

import (
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"agentmux/internal/app"
	"agentmux/internal/natmux"
	"agentmux/internal/store"
	"agentmux/internal/webserve"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	// The same executable is also the native session daemon: the broker that
	// keeps agents alive on hosts that have no tmux — native Windows. It is
	// spawned detached with this flag and must never start a window.
	if len(os.Args) > 1 && os.Args[1] == "--natmuxd" {
		natmux.DaemonMain()
		return
	}

	// Headless server mode: the same core and services, but served over HTTP
	// for browsers — tablets and phones — instead of bound to a native window.
	if len(os.Args) > 1 && os.Args[1] == "--serve" {
		serveMain(os.Args[2:])
		return
	}

	// The desktop build opens the native window here; the headless server
	// build, which links neither GTK nor a webview, serves instead.
	runApp()
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

// serveMain runs AgentMux as a headless web server: the built frontend, an RPC
// endpoint and an event stream behind a bearer token. This is what a tablet —
// Android or iPad — connects to; the browser is just another window onto the
// same core, and closing it stops nothing, exactly like closing the desktop.
//
// The window-bound services are deliberately absent: WindowService and
// DesktopService drive native webview windows that do not exist here, and
// UpdateService replaces the desktop binary.
func serveMain(args []string) {
	flags := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := flags.String("addr", envOr("AGENTMUX_ADDR", ":8642"), "listen address, e.g. :8642 or 0.0.0.0:8642")
	_ = flags.Parse(args)

	core, err := app.NewCore()
	if err != nil {
		fatal("AgentMux could not start", err)
	}
	agentSvc := app.NewAgentService(core)
	// The desktop service in serve mode offers the half that makes sense here:
	// probing a host and carrying a session to a viewer in the page. Opening
	// one in a viewer installed on this machine is the desktop app's business,
	// and would put a window on a screen nobody is sitting at.
	desktopSvc := app.NewDesktopService(core)
	services := []any{
		app.NewServerService(core),
		app.NewTreeService(core),
		app.NewTerminalService(core),
		app.NewTmuxService(core),
		app.NewToolkitService(core),
		app.NewFileService(core),
		app.NewMetricsService(core),
		app.NewLLMService(core),
		app.NewMemoryService(core),
		app.NewSkillService(core),
		app.NewOrchService(core),
		app.NewConfigService(core),
		app.NewUpdateCheckService(core),
		desktopSvc,
		agentSvc,
	}
	// The headless build can replace its own binary and adds the full update
	// service; the desktop binary running --serve cannot (its updater relaunches
	// a window), so there it adds nothing and updates stay check-only.
	services = append(services, serveUpdateExtras(core)...)
	registry := webserve.NewRegistry(services...)
	hub := webserve.NewHub()
	core.SetEmitter(hub.Emit)
	core.StartPoller(agentSvc, 5*time.Second)

	dataDir, err := store.AppDir()
	if err != nil {
		fatal("AgentMux could not find its data directory", err)
	}
	token, err := webserve.LoadOrCreateToken(dataDir)
	if err != nil {
		fatal("AgentMux could not establish a serve token", err)
	}
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		fatal("frontend assets missing from this build", err)
	}

	web := webserve.New(registry, hub, dist, token)
	web.Handle("GET "+app.WSPath, http.HandlerFunc(desktopSvc.ServeWS))
	server := &http.Server{Addr: *addr, Handler: web.Handler()}
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop
		_ = server.Close()
	}()

	log.Printf("AgentMux serving on %s", *addr)
	log.Printf("access token: %s", token)
	log.Printf("open http://<this-host>%s on your tablet and enter the token (or set AGENTMUX_TOKEN)", portOf(*addr))
	err = server.ListenAndServe()
	core.Shutdown()
	if err != nil && err != http.ErrServerClosed {
		fatal("AgentMux server exited with an error", err)
	}
}

// envOr reads an environment variable with a fallback.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// portOf extracts the ":port" part of a listen address for display.
func portOf(addr string) string {
	if i := len(addr) - 1; i >= 0 {
		for j := i; j >= 0; j-- {
			if addr[j] == ':' {
				return addr[j:]
			}
		}
	}
	return addr
}
