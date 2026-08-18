package sshx

import (
	"encoding/base64"
	"sort"
	"strings"
	"unicode/utf16"
)

// Remote Windows hosts are reached over the same SSH as everything else, but
// what is on the other end is cmd.exe or PowerShell — an operator's choice we
// cannot see from here. Everything in this file therefore avoids depending on
// it: a command is always `powershell.exe <plain flags> -EncodedCommand <b64>`,
// which is valid syntax under either default shell because it contains nothing
// either would interpret, and the script itself rides inside the base64 where
// no quoting rules apply.

// EncodePowerShell converts a script to the UTF-16LE base64 form that
// PowerShell's -EncodedCommand flag takes.
func EncodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	raw := make([]byte, 0, len(units)*2)
	for _, u := range units {
		raw = append(raw, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// PowerShellCommand wraps a one-shot script for execution on a Windows host:
// no profile, no prompts, exit when done.
func PowerShellCommand(script string) string {
	return "powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -EncodedCommand " +
		EncodePowerShell(script)
}

// PSQuote wraps a string in PowerShell single quotes, where the only escape is
// doubling the quote itself.
func PSQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// WinCommandLine is CommandLine for a Windows host: the same fold of working
// directory, environment and command into one line, phrased for PowerShell.
// It returns "" when a plain interactive shell is all that was asked for,
// which lets openSSH request a shell and get whatever the host considers its
// default — exactly as on POSIX.
func WinCommandLine(opts ShellOptions) string {
	var setup strings.Builder
	if len(opts.Env) > 0 {
		keys := make([]string, 0, len(opts.Env))
		for k := range opts.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			setup.WriteString("$env:" + k + " = " + PSQuote(opts.Env[k]) + "; ")
		}
	}
	if opts.Cwd != "" {
		setup.WriteString("Set-Location " + PSQuote(opts.Cwd) + "; ")
	}

	switch {
	case opts.Command != "":
		// A command runs and the terminal ends with it, as on POSIX.
		return "powershell.exe -NoLogo -ExecutionPolicy Bypass -EncodedCommand " +
			EncodePowerShell(setup.String()+opts.Command)
	case setup.Len() > 0:
		// Apply cwd and environment, then stay interactive.
		return "powershell.exe -NoLogo -ExecutionPolicy Bypass -NoExit -EncodedCommand " +
			EncodePowerShell(setup.String())
	default:
		return ""
	}
}
