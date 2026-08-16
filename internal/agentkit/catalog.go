// Package agentkit knows how to detect and install the AI agent CLIs people run
// on their servers, plus the runtimes those CLIs need.
//
// Installs are never run blind over a bare SSH exec: they go into a tmux
// session, so a dropped connection cannot leave a half-finished install, the
// user can watch the output, and interactive prompts — a sudo password, a
// licence agreement — are answerable.
package agentkit

// Method is one way to install a tool.
type Method struct {
	ID string `json:"id"`
	// Label is what the button says.
	Label string `json:"label"`
	// Requires names a binary that must exist for this method to be usable.
	// Empty means the method has no prerequisite beyond a shell.
	Requires string `json:"requires"`
	// NeedsRoot marks methods that will prompt for a sudo password.
	NeedsRoot bool `json:"needsRoot"`
	// Script is the shell command run inside the install tmux session.
	Script string `json:"script"`
}

// Tool is an installable agent CLI or runtime.
type Tool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Vendor      string `json:"vendor"`
	Description string `json:"description"`
	// Binary is the command probed to decide whether the tool is installed.
	Binary string `json:"binary"`
	// RunCommand is what an AgentMux agent should launch. Usually the binary.
	RunCommand string `json:"runCommand"`
	// VersionArgs is appended to Binary to read a version string.
	VersionArgs string   `json:"versionArgs"`
	Docs        string   `json:"docs"`
	Methods     []Method `json:"methods"`
	// Kind separates the agents people want from the plumbing they need.
	Kind string `json:"kind"` // agent | runtime
}

// Agents lists the agent CLIs offered for one-click install.
//
// The install commands are the vendors' documented ones. They are data, not
// logic: when a vendor changes their installer, this table is the only thing
// that needs editing, and users can always fall back to the custom script box.
func Agents() []Tool {
	return []Tool{
		{
			ID:          "claude-code",
			Name:        "Claude Code",
			Vendor:      "Anthropic",
			Description: "Anthropic's terminal coding agent.",
			Binary:      "claude",
			RunCommand:  "claude",
			VersionArgs: "--version",
			Docs:        "https://docs.claude.com/en/docs/claude-code/overview",
			Kind:        "agent",
			Methods: []Method{
				{
					ID:       "native",
					Label:    "Native installer",
					Requires: "curl",
					Script:   `curl -fsSL https://claude.ai/install.sh | bash`,
				},
				{
					ID:       "npm",
					Label:    "npm (global)",
					Requires: "npm",
					Script:   `npm install -g @anthropic-ai/claude-code`,
				},
			},
		},
		{
			ID:          "codex",
			Name:        "Codex CLI",
			Vendor:      "OpenAI",
			Description: "OpenAI's terminal coding agent.",
			Binary:      "codex",
			RunCommand:  "codex",
			VersionArgs: "--version",
			Docs:        "https://github.com/openai/codex",
			Kind:        "agent",
			Methods: []Method{
				{ID: "npm", Label: "npm (global)", Requires: "npm", Script: `npm install -g @openai/codex`},
			},
		},
		{
			ID:          "gemini-cli",
			Name:        "Gemini CLI",
			Vendor:      "Google",
			Description: "Google's open-source terminal agent.",
			Binary:      "gemini",
			RunCommand:  "gemini",
			VersionArgs: "--version",
			Docs:        "https://github.com/google-gemini/gemini-cli",
			Kind:        "agent",
			Methods: []Method{
				{ID: "npm", Label: "npm (global)", Requires: "npm", Script: `npm install -g @google/gemini-cli`},
			},
		},
		{
			ID:          "opencode",
			Name:        "OpenCode",
			Vendor:      "SST",
			Description: "Provider-agnostic open-source terminal agent.",
			Binary:      "opencode",
			RunCommand:  "opencode",
			VersionArgs: "--version",
			Docs:        "https://opencode.ai",
			Kind:        "agent",
			Methods: []Method{
				{ID: "script", Label: "Install script", Requires: "curl", Script: `curl -fsSL https://opencode.ai/install | bash`},
				{ID: "npm", Label: "npm (global)", Requires: "npm", Script: `npm install -g opencode-ai`},
			},
		},
		{
			ID:          "aider",
			Name:        "Aider",
			Vendor:      "Aider AI",
			Description: "Pair-programming agent that edits your git repo.",
			Binary:      "aider",
			RunCommand:  "aider",
			VersionArgs: "--version",
			Docs:        "https://aider.chat",
			Kind:        "agent",
			Methods: []Method{
				{ID: "uv", Label: "uv tool", Requires: "uv", Script: `uv tool install --force --python python3.12 aider-chat@latest`},
				{ID: "pipx", Label: "pipx", Requires: "pipx", Script: `pipx install aider-chat`},
			},
		},
		{
			ID:          "cursor-agent",
			Name:        "Cursor CLI",
			Vendor:      "Cursor",
			Description: "Cursor's headless coding agent.",
			Binary:      "cursor-agent",
			RunCommand:  "cursor-agent",
			VersionArgs: "--version",
			Docs:        "https://cursor.com/cli",
			Kind:        "agent",
			Methods: []Method{
				{ID: "script", Label: "Install script", Requires: "curl", Script: `curl https://cursor.com/install -fsS | bash`},
			},
		},
	}
}

// Runtimes lists the plumbing agents depend on, including tmux itself — without
// which AgentMux cannot keep anything alive on that server.
func Runtimes() []Tool {
	return []Tool{
		{
			ID:          "tmux",
			Name:        "tmux",
			Vendor:      "",
			Description: "Required by AgentMux: every agent runs inside a tmux session.",
			Binary:      "tmux",
			RunCommand:  "tmux",
			VersionArgs: "-V",
			Docs:        "https://github.com/tmux/tmux",
			Kind:        "runtime",
			Methods: []Method{
				{ID: "apt", Label: "apt", Requires: "apt-get", NeedsRoot: true, Script: `sudo apt-get update && sudo apt-get install -y tmux`},
				{ID: "dnf", Label: "dnf", Requires: "dnf", NeedsRoot: true, Script: `sudo dnf install -y tmux`},
				{ID: "yum", Label: "yum", Requires: "yum", NeedsRoot: true, Script: `sudo yum install -y tmux`},
				{ID: "pacman", Label: "pacman", Requires: "pacman", NeedsRoot: true, Script: `sudo pacman -S --noconfirm tmux`},
				{ID: "apk", Label: "apk", Requires: "apk", NeedsRoot: true, Script: `sudo apk add tmux`},
				{ID: "brew", Label: "Homebrew", Requires: "brew", Script: `brew install tmux`},
			},
		},
		{
			ID:          "node",
			Name:        "Node.js",
			Vendor:      "",
			Description: "Runtime for the npm-installed agents.",
			Binary:      "node",
			RunCommand:  "node",
			VersionArgs: "--version",
			Docs:        "https://nodejs.org",
			Kind:        "runtime",
			Methods: []Method{
				{
					ID:       "nvm",
					Label:    "nvm (no root)",
					Requires: "curl",
					Script: `curl -fsSL https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash && ` +
						`export NVM_DIR="$HOME/.nvm" && . "$NVM_DIR/nvm.sh" && nvm install --lts && node --version`,
				},
				{ID: "apt", Label: "apt", Requires: "apt-get", NeedsRoot: true, Script: `sudo apt-get update && sudo apt-get install -y nodejs npm`},
				{ID: "brew", Label: "Homebrew", Requires: "brew", Script: `brew install node`},
			},
		},
		{
			ID:          "uv",
			Name:        "uv",
			Vendor:      "Astral",
			Description: "Python package manager used to install Aider.",
			Binary:      "uv",
			RunCommand:  "uv",
			VersionArgs: "--version",
			Docs:        "https://docs.astral.sh/uv/",
			Kind:        "runtime",
			Methods: []Method{
				{ID: "script", Label: "Install script", Requires: "curl", Script: `curl -LsSf https://astral.sh/uv/install.sh | sh`},
			},
		},
		{
			ID:          "pipx",
			Name:        "pipx",
			Vendor:      "",
			Description: "Isolated Python app installer.",
			Binary:      "pipx",
			RunCommand:  "pipx",
			VersionArgs: "--version",
			Docs:        "https://pipx.pypa.io",
			Kind:        "runtime",
			Methods: []Method{
				{ID: "apt", Label: "apt", Requires: "apt-get", NeedsRoot: true, Script: `sudo apt-get update && sudo apt-get install -y pipx && pipx ensurepath`},
				{ID: "pip", Label: "pip --user", Requires: "python3", Script: `python3 -m pip install --user pipx && python3 -m pipx ensurepath`},
				{ID: "brew", Label: "Homebrew", Requires: "brew", Script: `brew install pipx && pipx ensurepath`},
			},
		},
	}
}

// All returns runtimes followed by agents.
func All() []Tool {
	return append(Runtimes(), Agents()...)
}

// ByID finds a tool in the catalog.
func ByID(id string) (Tool, bool) {
	for _, t := range All() {
		if t.ID == id {
			return t, true
		}
	}
	return Tool{}, false
}

// MethodByID finds an install method on a tool.
func MethodByID(t Tool, id string) (Method, bool) {
	for _, m := range t.Methods {
		if m.ID == id {
			return m, true
		}
	}
	return Method{}, false
}

// probeBinaries is the set of binaries the detection pass looks for: every
// tool's binary plus the package managers and helpers install methods need.
func probeBinaries() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(b string) {
		if b != "" && !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	for _, t := range All() {
		add(t.Binary)
		for _, m := range t.Methods {
			add(m.Requires)
		}
	}
	for _, b := range []string{"npm", "python3", "git", "curl", "sudo", "bash"} {
		add(b)
	}
	return out
}
