// Package catalog names the orchestrator's tool surface: what tools exist,
// what they do, and how dangerous they are.
//
// Nothing here can execute anything. The registry that binds these names to
// real calls, and the gate that decides whether a call happens at all, live in
// the orchestrator itself.
//
// It is a package of its own because two things need the same vocabulary and
// must not depend on each other: the engine, which offers these tools to a
// model, and skill validation, which has to reject a skill naming a tool that
// does not exist at the moment it is written rather than when it is followed.
package catalog

// Risk is how much damage a tool can do.
//
// It is a property of the tool, fixed where the tool is declared. It is
// deliberately not a property of a skill: a skill is an object the AI can
// write, so a boundary drawn there would be a boundary the AI can move.
type Risk string

const (
	// RiskRead cannot change anything. Runs without asking.
	RiskRead Risk = "read"
	// RiskAct changes something recoverable. Asks, unless the server is
	// explicitly trusted.
	RiskAct Risk = "act"
	// RiskDestructive ends processes or deletes data. Always asks.
	RiskDestructive Risk = "destructive"
)

// ToolMeta describes one tool to a person and to a model.
type ToolMeta struct {
	Name string `json:"name"`
	// Description is written for the model: what it does, and when it is the
	// right choice rather than a neighbouring tool.
	Description string `json:"description"`
	Risk        Risk   `json:"risk"`
}

// tools is the whole tool surface. A name absent from this list cannot be
// called, recommended by a skill, or offered to a model.
var tools = []ToolMeta{
	// --- read ---------------------------------------------------------------
	{"agents.list", "List every configured agent with its current status.", RiskRead},
	{"agents.logs", "Read the last N lines an agent printed in its tmux pane.", RiskRead},
	{"tmux.sessions", "List the tmux sessions on one server.", RiskRead},
	{"tmux.panes", "List the tmux panes on one server, with the command running in each.", RiskRead},
	{"tmux.capture", "Read the visible contents of one tmux pane.", RiskRead},
	{"metrics.sample", "Read CPU, memory, disk, network and GPU vitals from one server.", RiskRead},
	{"files.list", "List a directory on a server over SFTP.", RiskRead},
	{"files.read", "Read one file from a server over SFTP.", RiskRead},
	{"servers.list", "List configured servers and whether they are connected.", RiskRead},
	{"toolkit.detect", "Report which agent CLIs and runtimes are installed on a server.", RiskRead},
	{"memory.search", "Search the memory library by meaning.", RiskRead},

	// --- act ----------------------------------------------------------------
	{"agents.send", "Type a message into an agent's pane. Set execute=false to leave it unsent for a human to review.", RiskAct},
	{"agents.start", "Start an agent in its tmux session, or attach to the session if it is already running.", RiskAct},
	{"agents.stop", "Ask an agent to stop, leaving its tmux session alive.", RiskAct},
	{"tmux.send_text", "Type text into a tmux session that has no agent record.", RiskAct},
	{"tmux.create_session", "Create a tmux session on a server.", RiskAct},
	{"files.write", "Write a file on a server over SFTP.", RiskAct},
	{"memory.write", "Remember a fact for later retrieval.", RiskAct},

	// --- destructive --------------------------------------------------------
	{"agents.kill", "Kill an agent's tmux session, destroying everything running inside it.", RiskDestructive},
	{"agents.restart", "Stop an agent and start it again, losing whatever it was mid-way through.", RiskDestructive},
	{"tmux.kill_session", "Kill a tmux session and every window and pane in it.", RiskDestructive},
	{"files.remove", "Delete a file or directory on a server.", RiskDestructive},
	{"agents.broadcast", "Send one message to many agents at once. The blast radius grows with the target count.", RiskDestructive},
	{"toolkit.install", "Install an agent CLI or runtime on a server.", RiskDestructive},
}

// All returns every known tool.
func All() []ToolMeta {
	out := make([]ToolMeta, len(tools))
	copy(out, tools)
	return out
}

// Lookup finds a tool by name.
func Lookup(name string) (ToolMeta, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return ToolMeta{}, false
}

// Known reports whether a name is a real tool.
func Known(name string) bool {
	_, ok := Lookup(name)
	return ok
}
