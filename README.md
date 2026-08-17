<img src="build/appicon/icon-128.png" width="96" align="right" alt="AgentMux">

# AgentMux

**A desktop control room for AI coding agents running on other people's computers.**

English · [中文](README.zh-CN.md)

![AgentMux](docs/demo.gif)

---

## The problem it solves

Coding agents do their best work on real machines — the build server, the GPU
box, the staging host. So you SSH in, start `claude` or `codex` or `aider`, and
now you are holding it. Close the laptop and the connection drops and the work
dies with it. Run five of them across three servers and you have become the
scheduler: a `tmux` cheat sheet, a folder of aliases, and a notes file recording
which agent is doing what where.

AgentMux is a window onto work that is already running somewhere else. Every
agent it starts runs inside a `tmux` session **on the server**. The app never
owns the process, so closing it, losing wifi, or putting the laptop in a bag
changes nothing about what the agent is doing.

## Why this one

**Closing a window is detaching, not killing.** This is the whole design, and
everything else follows from it. Open an agent's terminal and you are attached
to its own tmux pane; close the tab and it keeps working. You stop being careful
with windows.

![Terminal attached to an agent](docs/terminal.png)

**A real terminal, not a chat box with a terminal theme.** Colours, mouse,
selection, search, and the same pane the agent is typing into — so you can take
over mid-task, correct it, and hand it back without restarting anything.

**One instruction, one receipt per agent.** Tick twenty agents, send once, and
get twenty answers saying whether the message actually landed. A fan-out you
cannot verify is just hope.

![Broadcast receipts](docs/broadcast.png)

**A local orchestrator that has to ask.** Give it a goal — *find out why the
payment agent is stuck* — and it reads the fleet, recalls what it knows, and
works through it one tool call at a time. Anything that changes a server stops
and asks first, showing the tool, the arguments, the machine, and its own
sentence explaining why. Killing sessions and deleting files are confirmed on
every server, including the ones you marked trusted. It runs on a local model
through Ollama: no goal, no log line and no memory leaves the machine. And it is
off until you switch it on.

**It remembers, and shows you what it remembers.** Facts about a project,
preferences you have stated, what agents did — searchable by wording or by
meaning. Credentials are stripped before anything is stored, because the real
risk is not a stolen database file, it is the same token being handed back to a
model on every retrieval that matches.

**Procedures you approve, reused.** Write down what to do when the payment
service slows under load, and it gets matched and followed next time. Skills the
orchestrator proposes for itself arrive as drafts and cannot influence anything
until you approve them.

**A new box is not a chore.** The install panel looks at a server and offers only
what it can actually run — no npm on the host means the npm-based agents stay
hidden, and it says why. Installs run inside tmux, so a dropped connection cannot
leave you with half a package tree.

![Install panel](docs/install.png)

**It looks like it belongs on the machine.** Apple's system palette by default,
with segmented controls, sheets and switches rather than web conventions — and
six other themes including Nord, Solarized and a proper light one.

![Themes](docs/themes.png)

## Where it fits

- **Long jobs you cannot babysit.** A four-hour migration or refactor keeps
  running through a closed laptop, a train tunnel and a coffee break.
- **More agents than you can hold in your head.** Three to fifty boxes, each with
  work in flight, in one tree with a green dot for what is running and the last
  line each agent printed.
- **Fleet-wide instructions.** "Stop, pull main, rerun the tests" to everything
  at once — with proof of what landed.
- **Provisioning.** New server, no agent CLI, no runtime: pick from the list and
  let it install.
- **Unattended watching.** Patrols can look around on a timer and report an agent
  that has been stuck for an hour. A patrol may only read, whatever any server's
  trust level says.
- **Work that cannot leave the building.** The planner, the embeddings and the
  memory library are all local. AgentMux does not proxy your agent's model
  traffic or read its API keys.

## Install

Every release carries a build for each platform, made by GitHub Actions from the
tagged commit, with a `.sha256` beside it.

| File | What it is |
|---|---|
| [`agentmux-macos-universal.zip`](../../releases/latest) | `AgentMux.app`, one binary for Intel and Apple silicon (macOS 11+) |
| [`agentmux-windows-amd64.zip`](../../releases/latest) | `agentmux.exe` plus an install script |
| [`agentmux-linux-amd64.tar.gz`](../../releases/latest) | the binary, an icon, a `.desktop` file and `install.sh` |

On the remote side you need `tmux` and an SSH account, and AgentMux will install
tmux for you if it is missing.

**Linux** also needs GTK3 and WebKitGTK 4.1 present to run — `libwebkit2gtk-4.1-0`
on Debian and Ubuntu, `webkit2gtk4.1` on Fedora. Most desktops already have them,
and `install.sh` names what is missing rather than leaving you with a binary that
exits without a word.

**The builds are not notarized or code-signed**, which costs an Apple developer
account and a Microsoft certificate. On Windows, click "More info" then "Run
anyway". On macOS, what works depends on the version:

| Your macOS | What to do |
|---|---|
| 15 Sequoia or newer | Open it, let it be blocked, then System Settings → Privacy & Security → Security → **Open Anyway** |
| 14 Sonoma or older | Control-click the app → **Open** → Open |
| any version | `xattr -dr com.apple.quarantine /Applications/AgentMux.app`, then open it normally |

That quarantine flag is attached by the browser, not by the archive: downloading
with `curl -L -O <url>` means there is nothing to clear.

## First run

1. **Add a server.** Host, user, and ssh-agent, a key or a password. Jump hosts
   are supported. The host key is pinned on first connection.
2. **Add a project and a workspace** — a directory on that server.
3. **Add an agent**: a name and the command you run (`claude`, `codex`, whatever
   it is). Press Start.
4. `Ctrl+K` opens the command palette, which is the fastest way to attach to an
   agent, open a shell, install a CLI or switch theme.

Passwords and key passphrases are encrypted with AES-256-GCM before they touch
disk, and the key lives in the OS keychain — Credential Manager on Windows,
Keychain on macOS, Secret Service on Linux. If the keychain cannot be reached,
AgentMux falls back to a `0600` file and says so in the status bar, because a
weaker guarantee should never be a silent one.

## What it deliberately does not do

It does not replace your terminal, your editor or your agent, and it does not sit
between an agent and its model. It starts things, watches them, talks to them,
and gets out of the way.

Deleting anything in the app never kills remote work. The only button that
destroys a running session is called Kill, and it asks first.

## How it works

One SSH connection per server, multiplexed: every terminal, command and file
transfer is a channel on the same connection, so ten terminals on one host is one
login. Connections idle out after ten minutes unless something is watching them.

Agents run in a login shell inside tmux rather than as the session's own process,
so an agent that exits leaves you a shell in the right directory instead of
destroying the session.

State is one SQLite file in your application data directory. There is no server,
no daemon and no account.

## Building, testing, releasing

See [docs/development.md](docs/development.md).

The orchestrator's design — the tool gate, the trust levels, the memory and skill
layers, and the reasoning behind each — is written up in
[docs/orchestrator-blueprint.md](docs/orchestrator-blueprint.md) (Chinese).

## License

MIT. See [LICENSE](LICENSE).
