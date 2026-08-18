// Package natmux is AgentMux's own session daemon: a stand-in for tmux on the
// one platform that cannot have one. Native Windows development — MSVC builds,
// WPF test runs, anything that must happen outside WSL — needs agents that live
// in Windows itself, and the promise the rest of this application makes ("close
// the window, the work keeps running") is exactly the promise tmux delivers
// elsewhere. So this package delivers the same promise the same way: a small
// broker process, detached from the desktop application, owns every session's
// pseudo-terminal and scrollback; the application is only ever a client of it.
//
// The daemon is this very executable run with --natmuxd. It listens on a named
// pipe restricted to the current user (a unix socket elsewhere, which is what
// the tests exercise), and speaks newline-delimited JSON: session CRUD, typed
// input, capture, and an attach that upgrades the connection to a raw
// bidirectional stream. Sessions run the platform's native shell — PowerShell
// on Windows — in a real ConPTY, so agents get colour, mouse reporting and
// interactive prompts exactly as they do inside tmux.
package natmux

import (
	"hash/crc32"
	"strings"
)

// Version names the protocol generation, reported by ping so a mismatched
// daemon left over from an older build can be recognised.
const Version = "1"

// request is one client instruction to the daemon.
type request struct {
	Op    string `json:"op"`
	Name  string `json:"name,omitempty"`
	To    string `json:"to,omitempty"`
	Cwd   string `json:"cwd,omitempty"`
	B64   string `json:"b64,omitempty"`
	Key   string `json:"key,omitempty"`
	Enter bool   `json:"enter,omitempty"`
	Lines int    `json:"lines,omitempty"`
	Cols  int    `json:"cols,omitempty"`
	Rows  int    `json:"rows,omitempty"`
	// Token authenticates a TCP connection (op "auth"). The named pipe and the
	// unix socket carry their access control in the transport and need none.
	Token string `json:"token,omitempty"`
}

// TCPPort is the loopback port the daemon serves for one user. Deterministic on
// the username, so a client arriving over SSH can compute it without asking;
// spread across a range so two accounts on one machine do not collide.
func TCPPort(user string) int {
	return 47000 + int(crc32.ChecksumIEEE([]byte(strings.ToLower(user)))%2000)
}

// response answers one request.
type response struct {
	OK       bool          `json:"ok"`
	Error    string        `json:"error,omitempty"`
	Version  string        `json:"version,omitempty"`
	Exists   bool          `json:"exists,omitempty"`
	B64      string        `json:"b64,omitempty"`
	Sessions []SessionInfo `json:"sessions,omitempty"`
}

// SessionInfo is what the daemon knows about one session.
type SessionInfo struct {
	Name     string `json:"name"`
	Cwd      string `json:"cwd"`
	Created  int64  `json:"created"`
	Activity int64  `json:"activity"`
	Attached int    `json:"attached"`
	PID      int    `json:"pid"`
	// Command is the process currently in the foreground of the session: the
	// shell's own name when it is sitting at a prompt, the agent's when one is
	// working. It answers the same question tmux's pane_current_command does.
	Command string `json:"command"`
}

// frame is one message on an attached connection, in either direction.
// Client to daemon: data, resize, detach. Daemon to client: data, exit.
type frame struct {
	Stream string `json:"stream"`
	B64    string `json:"b64,omitempty"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
	Reason string `json:"reason,omitempty"`
}
