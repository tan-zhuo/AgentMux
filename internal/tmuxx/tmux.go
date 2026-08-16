// Package tmuxx wraps the remote tmux CLI. Everything AgentMux runs on a server
// goes through a tmux session, so that losing the desktop connection never kills
// an agent.
package tmuxx

import (
	"fmt"
	"strconv"
	"strings"

	"agentmux/internal/sshx"
)

// Runner executes a command on a server. *sshx.Pool satisfies it.
type Runner interface {
	Exec(serverID, cmd string) (sshx.ExecResult, error)
}

// Client issues tmux commands against one or more servers.
type Client struct {
	run Runner
}

// New builds a tmux client on top of a command runner.
func New(run Runner) *Client { return &Client{run: run} }

// Session is a remote tmux session.
type Session struct {
	Name     string `json:"name"`
	Windows  int    `json:"windows"`
	Attached bool   `json:"attached"`
	Created  int64  `json:"created"`
	Activity int64  `json:"activity"`
}

// Pane is one tmux pane, the finest-grained thing AgentMux addresses.
type Pane struct {
	SessionName string `json:"sessionName"`
	WindowIndex string `json:"windowIndex"`
	WindowName  string `json:"windowName"`
	PaneIndex   string `json:"paneIndex"`
	PaneID      string `json:"paneId"`
	PID         int    `json:"pid"`
	Command     string `json:"command"`
	Path        string `json:"path"`
	Active      bool   `json:"active"`
	Title       string `json:"title"`
}

// Info summarises tmux availability on a server.
type Info struct {
	Available bool   `json:"available"`
	Version   string `json:"version"`
	Error     string `json:"error"`
}

// sep separates fields in tmux -F output.
//
// It cannot be a control character: when tmux writes command output to a client
// that is not a terminal — which is exactly what an SSH exec channel is — it
// escapes control bytes. A tab comes back as '_' and 0x1f as the literal text
// "\037", which silently turns every parsed row into one useless field. A
// printable marker passes through byte for byte, and it is improbable enough
// that it will not show up inside a session name or a path.
const sep = "~@~"

// q quotes a value for the remote /bin/sh.
func q(s string) string { return sshx.ShellQuote(s) }

// target builds an exact-match tmux target so a session named "web" never
// matches "web-2" through tmux's default fnmatch behaviour.
func target(session string, extra ...string) string {
	t := "=" + session
	if len(extra) > 0 && extra[0] != "" {
		t += ":" + extra[0]
	}
	if len(extra) > 1 && extra[1] != "" {
		t += "." + extra[1]
	}
	return t
}

// Available reports whether tmux is installed on the server.
func (c *Client) Available(serverID string) Info {
	res, err := c.run.Exec(serverID, `tmux -V 2>/dev/null || true`)
	if err != nil {
		return Info{Error: err.Error()}
	}
	v := strings.TrimSpace(res.Stdout)
	if v == "" {
		return Info{Available: false, Error: "tmux is not installed or not on PATH"}
	}
	return Info{Available: true, Version: v}
}

// ListSessions returns every tmux session on a server. A server with no tmux
// daemon running yields an empty list rather than an error.
func (c *Client) ListSessions(serverID string) ([]Session, error) {
	// session_name goes last: it is user-supplied, so if a name ever contains the
	// separator, SplitN keeps the damage inside the final field.
	format := strings.Join([]string{
		"#{session_windows}", "#{session_attached}",
		"#{session_created}", "#{session_activity}", "#{session_name}",
	}, sep)

	res, err := c.run.Exec(serverID, `tmux list-sessions -F `+q(format))
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		if noServer(res.Stderr) {
			return []Session{}, nil
		}
		return nil, fmt.Errorf("tmux list-sessions: %s", strings.TrimSpace(res.Stderr))
	}

	out := []Session{}
	for _, line := range splitLines(res.Stdout) {
		f := strings.SplitN(line, sep, 5)
		if len(f) < 5 {
			continue
		}
		out = append(out, Session{
			Windows:  atoi(f[0]),
			Attached: f[1] != "0",
			Created:  int64(atoi(f[2])),
			Activity: int64(atoi(f[3])),
			Name:     f[4],
		})
	}
	return out, nil
}

// ListPanes returns every pane across every session on a server, which is one
// round trip for the whole server view.
func (c *Client) ListPanes(serverID string) ([]Pane, error) {
	// Machine-generated fields first, free-form ones last, so an embedded
	// separator in a title or a path cannot shift the columns before it.
	format := strings.Join([]string{
		"#{window_index}", "#{pane_index}", "#{pane_id}", "#{pane_pid}",
		"#{pane_active}", "#{pane_current_command}",
		"#{session_name}", "#{window_name}", "#{pane_current_path}", "#{pane_title}",
	}, sep)

	res, err := c.run.Exec(serverID, `tmux list-panes -a -F `+q(format))
	if err != nil {
		return nil, err
	}
	if res.Code != 0 {
		if noServer(res.Stderr) {
			return []Pane{}, nil
		}
		return nil, fmt.Errorf("tmux list-panes: %s", strings.TrimSpace(res.Stderr))
	}

	out := []Pane{}
	for _, line := range splitLines(res.Stdout) {
		f := strings.SplitN(line, sep, 10)
		if len(f) < 10 {
			continue
		}
		out = append(out, Pane{
			WindowIndex: f[0], PaneIndex: f[1], PaneID: f[2], PID: atoi(f[3]),
			Active: f[4] != "0", Command: f[5],
			SessionName: f[6], WindowName: f[7], Path: f[8], Title: f[9],
		})
	}
	return out, nil
}

// HasSession reports whether a session with the exact name exists.
func (c *Client) HasSession(serverID, name string) (bool, error) {
	res, err := c.run.Exec(serverID, `tmux has-session -t `+q(target(name))+` 2>/dev/null`)
	if err != nil {
		return false, err
	}
	return res.Code == 0, nil
}

// NewSession creates a detached session rooted at cwd. The first pane runs the
// login shell rather than the agent command directly: when an agent exits the
// pane survives, keeping its scrollback and letting the user take over manually.
func (c *Client) NewSession(serverID, name, cwd string) error {
	if strings.ContainsAny(name, ":.") {
		return fmt.Errorf("tmux session name %q cannot contain ':' or '.'", name)
	}
	cmd := `tmux new-session -d -s ` + q(name)
	if cwd != "" {
		cmd += ` -c ` + q(cwd)
	}
	res, err := c.run.Exec(serverID, cmd)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("tmux new-session: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// KillSession terminates a session and everything running inside it.
func (c *Client) KillSession(serverID, name string) error {
	res, err := c.run.Exec(serverID, `tmux kill-session -t `+q(target(name)))
	if err != nil {
		return err
	}
	if res.Code != 0 && !noServer(res.Stderr) {
		return fmt.Errorf("tmux kill-session: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// RenameSession renames a session in place.
func (c *Client) RenameSession(serverID, from, to string) error {
	if strings.ContainsAny(to, ":.") {
		return fmt.Errorf("tmux session name %q cannot contain ':' or '.'", to)
	}
	res, err := c.run.Exec(serverID, `tmux rename-session -t `+q(target(from))+` `+q(to))
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("tmux rename-session: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// SendText types literal text into a pane. The -l flag stops tmux from
// interpreting words like "Enter" or "C-c" that may appear inside an agent
// prompt, so a message to an agent is delivered verbatim.
func (c *Client) SendText(serverID, tgt, text string, pressEnter bool) error {
	cmd := `tmux send-keys -t ` + q(tgt) + ` -l -- ` + q(text)
	if pressEnter {
		cmd += ` && tmux send-keys -t ` + q(tgt) + ` Enter`
	}
	res, err := c.run.Exec(serverID, cmd)
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("tmux send-keys: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// SendKey sends a named key such as "C-c", "Escape" or "Up" to a pane.
func (c *Client) SendKey(serverID, tgt, key string) error {
	res, err := c.run.Exec(serverID, `tmux send-keys -t `+q(tgt)+` `+q(key))
	if err != nil {
		return err
	}
	if res.Code != 0 {
		return fmt.Errorf("tmux send-keys: %s", strings.TrimSpace(res.Stderr))
	}
	return nil
}

// CapturePane returns the last n lines of a pane's scrollback, used for status
// polling and the log panel without attaching a PTY.
func (c *Client) CapturePane(serverID, tgt string, lines int) (string, error) {
	if lines <= 0 {
		lines = 200
	}
	cmd := `tmux capture-pane -p -J -t ` + q(tgt) + ` -S -` + strconv.Itoa(lines)
	res, err := c.run.Exec(serverID, cmd)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", fmt.Errorf("tmux capture-pane: %s", strings.TrimSpace(res.Stderr))
	}
	return res.Stdout, nil
}

// AttachCommand is the remote command that attaches a PTY to a session. It is
// run inside an sshx PTY session rather than through Exec.
func AttachCommand(name string) string {
	// -A creates the session if it vanished; -D detaches other clients so two
	// AgentMux windows do not fight over the same terminal size.
	return `tmux attach-session -t ` + sshx.ShellQuote(target(name))
}

// Target exposes the exact-match target syntax to other packages.
func Target(session string, extra ...string) string { return target(session, extra...) }

func noServer(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "no server running") ||
		strings.Contains(s, "error connecting to") ||
		strings.Contains(s, "no current session")
}

func splitLines(s string) []string {
	raw := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
