package localx

import (
	"runtime"
	"strings"
)

// withUTF8Locale makes sure a child process on this machine sees a UTF-8
// locale, filling one in only when the environment has none at all.
//
// A Mac application started from the Dock or from Finder inherits launchd's
// environment, and launchd has no LANG in it. Terminal.app is what normally
// sets one, and AgentMux is not Terminal.app — so every shell it opens, and
// every agent running inside one, lands in the C locale. There a Chinese
// character is measured as one byte-wide column and text arriving from an
// input method is bytes the program will not take: the agent's prompt refuses
// Chinese outright, and whatever does get through comes back misaligned.
//
// Only an environment with no locale at all is filled in, so anyone who has
// deliberately set one keeps it, whatever it says. LC_CTYPE is the narrowest
// key that answers this: it decides how characters are read and how wide they
// are, and leaves the language of messages alone. "UTF-8" is Darwin's own name
// for the codeset-only locale — the value Terminal.app itself falls back to —
// and is not a locale name elsewhere, which is why this is macOS only. On
// Linux the desktop session sets LANG, and inventing a name that a given libc
// may not carry would trade a real fix for a warning on every command.
func withUTF8Locale(env []string) []string {
	return withUTF8LocaleFor(env, runtime.GOOS)
}

func withUTF8LocaleFor(env []string, goos string) []string {
	if goos != "darwin" {
		return env
	}
	for _, kv := range env {
		name, value, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		switch name {
		case "LC_ALL", "LC_CTYPE", "LANG":
			return env
		}
	}
	return append(env, "LC_CTYPE=UTF-8")
}
