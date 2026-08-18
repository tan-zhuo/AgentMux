package app

import (
	"sort"
	"strings"

	"agentmux/internal/store"
)

// The native Windows host speaks PowerShell where every other host speaks a
// POSIX sh. Anything that composes a command line to type into a session asks
// the host's kind and uses one of these builders; nothing else in the
// application needs to know the two dialects exist.

// psQuote wraps a string in PowerShell single quotes, where the only escape is
// doubling the quote itself.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// buildLaunchCommandWin is buildLaunchCommand in PowerShell: environment first,
// then the working directory, then the agent command, so a pre-existing session
// still lands in the right place.
func buildLaunchCommandWin(ws store.Workspace, command string) string {
	var b strings.Builder
	keys := make([]string, 0, len(ws.Env))
	for k := range ws.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString("$env:" + k + " = " + psQuote(ws.Env[k]) + "; ")
	}
	if ws.RemotePath != "" {
		b.WriteString("Set-Location " + psQuote(ws.RemotePath) + "; ")
	}
	b.WriteString(command)
	return b.String()
}
