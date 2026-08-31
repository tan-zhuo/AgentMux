// The server build has no window to open: launched bare, it serves. This keeps
// the deployment story on a fresh box to one step — copy the binary, run it —
// with --serve flags still accepted for anyone scripting both builds alike.
//go:build headless

package main

import (
	"log"
	"os"
	"time"

	"agentmux/internal/app"
)

func runApp() {
	log.Print("headless build: starting serve mode (this binary has no desktop window)")
	serveMain(os.Args[1:])
}

// serveUpdateExtras gives serve mode the self-updating service: this build is
// the very artifact the release publishes for servers, so it can download the
// next one, swap itself and exec over. The quiet daily rhythm matches the
// desktop's; anyone impatient can ask from the settings dialog.
func serveUpdateExtras(core *app.Core) []any {
	u := app.NewUpdateService(core)
	u.StartWatch(6 * time.Hour)
	return []any{u}
}
