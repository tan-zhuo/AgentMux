package app

import (
	"strings"

	"agentmux/internal/store"
)

// classifyActivity reads a pane capture and decides what the agent in it is
// doing: actively working, blocked on a question for the human, or sitting
// quietly at its prompt with nothing to do.
//
// This is heuristic by nature — an agent CLI is a full-screen TUI and the only
// window into it is its text. The patterns below are the stable furniture of
// the popular agents (Claude Code, Codex, Gemini CLI, Aider): the "esc to
// interrupt" hint that is on screen exactly as long as work is running, the
// yes/no and numbered-option dialogs that sit there until answered. Unknown
// agents degrade gracefully: with no pattern matched they read as quiet, which
// flags them as "not doing anything" rather than inventing urgency.
func classifyActivity(capture string) store.AgentActivity {
	tail := tailLines(capture, 20)
	if len(tail) == 0 {
		return store.ActivityQuiet
	}

	// A question outranks a spinner. Approval dialogs keep working-style hints
	// on screen ("esc to cancel"), but a dialog means nothing moves until a
	// person answers, and that is the fact worth surfacing.
	for _, line := range tail {
		if lineAsksForInput(line) {
			return store.ActivityInput
		}
	}
	for _, line := range tail {
		if lineShowsWork(line) {
			return store.ActivityWorking
		}
	}
	return store.ActivityQuiet
}

// tailLines returns the last n non-blank lines, lowercased for matching.
func tailLines(capture string, n int) []string {
	lines := strings.Split(strings.ReplaceAll(capture, "\r\n", "\n"), "\n")
	out := make([]string, 0, n)
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		out = append(out, strings.ToLower(l))
	}
	return out
}

// inputMarkers are phrases that only appear while an agent is holding a
// question open: permission dialogs, yes/no confirmations, option menus.
var inputMarkers = []string{
	"do you want",      // Claude Code permission dialogs
	"would you like",   // Claude Code / Gemini CLI questions
	"waiting for your", // generic "waiting for your input/approval"
	"awaiting your",    //
	"needs your approval",
	"allow command",                          // Codex approval dialog
	"approve this",                           //
	"press enter to",                         // pagers and confirm gates
	"(y/n)", "[y/n]", "(yes/no)", "[yes/no]", // classic confirms
	"(y)es", // Aider: "Apply edit? (Y)es/(N)o"
}

// choiceMarkers mark a selected first entry of a numbered options menu — the
// shape every TUI agent draws for "pick one".
var choiceMarkers = []string{"❯ 1.", "› 1.", "> 1."}

func lineAsksForInput(line string) bool {
	for _, m := range inputMarkers {
		if strings.Contains(line, m) {
			return true
		}
	}
	for _, m := range choiceMarkers {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

// workMarkers are on screen exactly as long as the agent is busy, and are
// erased from the live pane the moment it stops.
var workMarkers = []string{
	"esc to interrupt", // Claude Code / Codex while running
	"ctrl+c to interrupt",
	"ctrl-c to interrupt",
	"esc to cancel", // Gemini CLI while running
	"esc again to interrupt",
}

// spinnerRunes are the braille spinner frames the common TUIs animate while
// working. One of them sitting in the pane means a live spinner.
const spinnerRunes = "⠁⠂⠄⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏⠟⠻⠽⠾⠷⠯⡿⣟⣯⣷⣾⣽⣻⢿"

func lineShowsWork(line string) bool {
	for _, m := range workMarkers {
		if strings.Contains(line, m) {
			return true
		}
	}
	return strings.ContainsAny(line, spinnerRunes)
}
