// The server build has no window to open: launched bare, it serves. This keeps
// the deployment story on a fresh box to one step — copy the binary, run it —
// with --serve flags still accepted for anyone scripting both builds alike.
//go:build headless

package main

import (
	"log"
	"os"
)

func runApp() {
	log.Print("headless build: starting serve mode (this binary has no desktop window)")
	serveMain(os.Args[1:])
}
