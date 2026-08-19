package app

import (
	"testing"

	"agentmux/internal/store"
)

func TestClassifyActivity(t *testing.T) {
	cases := []struct {
		name    string
		capture string
		want    store.AgentActivity
	}{
		{
			name: "claude code permission dialog",
			capture: `● Bash(rm -rf build)

Do you want to proceed?
❯ 1. Yes
  2. Yes, and don't ask again
  3. No, and tell Claude what to do differently`,
			want: store.ActivityInput,
		},
		{
			name: "numbered menu with selector",
			capture: `Pick a model:
› 1. sonnet
  2. opus`,
			want: store.ActivityInput,
		},
		{
			name:    "classic y/n confirm",
			capture: "Apply edit to main.py? (Y)es/(N)o [Yes]:",
			want:    store.ActivityInput,
		},
		{
			name: "claude code busy line",
			capture: `● Reading files…

✶ Churning… (esc to interrupt · 32s · 4.1k tokens)`,
			want: store.ActivityWorking,
		},
		{
			name:    "codex busy line",
			capture: "▌ Working (12s • Esc to interrupt)",
			want:    store.ActivityWorking,
		},
		{
			name:    "braille spinner",
			capture: "⠹ Generating response…",
			want:    store.ActivityWorking,
		},
		{
			name: "idle at an empty prompt",
			capture: `● Done. The tests pass now.

╭──────────────────────────────╮
│ >                            │
╰──────────────────────────────╯
  ? for shortcuts`,
			want: store.ActivityQuiet,
		},
		{
			name:    "empty capture",
			capture: "",
			want:    store.ActivityQuiet,
		},
		{
			// The dialog's own "esc to cancel" footer must not beat the
			// question: a held dialog is blocked on a human, not working.
			name: "dialog outranks work hints",
			capture: `Do you want to allow this?
❯ 1. Yes
  2. No
esc to cancel`,
			want: store.ActivityInput,
		},
		{
			// Old scrollback far above the tail must not fire: only the last
			// lines describe what the pane is doing now.
			name: "stale prompt above a working tail",
			capture: `Do you want to proceed?
1
2
3
4
5
6
7
8
9
10
11
12
13
14
15
16
17
18
19
20
✶ Working… (esc to interrupt)`,
			want: store.ActivityWorking,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyActivity(tc.capture); got != tc.want {
				t.Fatalf("classifyActivity() = %q, want %q", got, tc.want)
			}
		})
	}
}
