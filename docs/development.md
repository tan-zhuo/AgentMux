# Development

Building AgentMux, testing it, and cutting a release.

## Prerequisites

Go 1.25+ and Node 20+. On the remote side, the integration tests want an SSH
account with `tmux` on it, but they skip themselves without one.

## Building

```sh
git clone git@github.com:tan-zhuo/AgentMux.git
cd AgentMux
cd frontend && npm install && npm run build && cd ..
go build -o agentmux .
./agentmux
```

The frontend is embedded in the binary, so `npm run build` has to happen before
`go build`.

### Linux

The webview is WebKitGTK, so its headers have to be there at compile time.
AgentMux builds against GTK3 and webkit2gtk-4.1, which is what desktops have
installed today — Wails would otherwise reach for GTK4 and webkitgtk-6.0, which
only exist from Ubuntu 24.04 onwards, and which few machines have at run time.

```sh
sudo apt-get install -y build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
go build -tags gtk3 -o agentmux .
```

The same `-tags gtk3` belongs on `go vet` and `go test`. macOS and Windows use
their own webview — WKWebView and WebView2 — and need nothing installed; the tag
is ignored there.

> The GTK3 path is deprecated upstream and disappears in Wails v3.1. Moving to
> GTK4 means building on Ubuntu 24.04 with `libgtk-4-dev libwebkitgtk-6.0-dev`,
> dropping the tag, and accepting a higher glibc floor.

### Windows

A plain `go build` produces a console binary, which is what you want while
developing — the Wails log goes to the terminal. For something to double-click,
link it as a GUI binary so no console window opens behind the app:

```sh
go build -ldflags "-H windowsgui" -o agentmux.exe .
```

Then stderr goes nowhere, so a failure to start would be silent. AgentMux writes
those to `startup-error.log` in its data directory instead.

For a proper install — `%LOCALAPPDATA%\Programs\AgentMux`, desktop and Start menu
shortcuts, no administrator rights, and without touching the data in
`%APPDATA%\AgentMux`:

```powershell
powershell -ExecutionPolicy Bypass -File scripts\install-windows.ps1 -Build
```

`-Uninstall` reverses it.

## Testing

```sh
go test -tags gtk3 ./...
cd frontend && npm run lint && npm run build
```

Most of the suite is offline and hermetic: every test that touches the database
points `AGENTMUX_DATA_DIR` at a temporary directory, and everything that needs a
model uses a fake embedder or a scripted chat client rather than Ollama.

Two things are worth knowing about how the tests are written:

**Anything environment-dependent is pinned twice.** The runtime diagnosis, for
example, is asserted against synthetic errors in the `llm` package — because how
a kernel words a failed connection differs between machines, and a CI runner once
disagreed with a laptop about which branch was taken.

**The orchestrator is tested by running the loop.** `internal/orch` drives whole
runs against a scripted model: a destructive call parks the run and nothing
executes until a decision arrives; a patrol is refused outright rather than
queued; hostile tool output reaches the model wrapped and flagged. Those
assertions are about behaviour, not about parts.

The SSH, tmux and SFTP integration tests need a real host and skip themselves
unless one is configured:

```sh
AGENTMUX_TEST_HOST=127.0.0.1 AGENTMUX_TEST_PORT=2222 \
AGENTMUX_TEST_USER=you AGENTMUX_TEST_KEY=/path/to/key \
  go test -tags gtk3 ./internal/integration -v
```

`AGENTMUX_DATA_DIR` also works outside tests: point it somewhere else to get a
separate profile — a scratch instance that cannot touch the servers, keys and
layout of your real one.

## Cutting a release

Two workflows:

- **CI** runs on every push and pull request: frontend lint and build, then
  `go vet`, `go build` and `go test` on Linux, macOS and Windows.
- **Release** builds the three archives. It triggers on a *published* release —
  pushing a tag alone does nothing — and can also be run by hand from the Actions
  tab, which produces the same archives as workflow artifacts without touching
  any release.

Rehearse before publishing:

```sh
gh workflow run release.yml --ref main
gh run watch
```

Then tag and publish:

```sh
git tag -a v0.2.0 -m "what changed"
git push origin v0.2.0
gh release create v0.2.0 --title v0.2.0 --generate-notes
```

The workflow builds the tagged commit, stamps the tag into the binary through
`-ldflags` (Settings shows it), and attaches the three archives with their
`.sha256` files.

macOS is built twice, for `arm64` and `x86_64`, and joined with `lipo` into one
universal binary. The builds are ad-hoc signed, which is what lets an arm64
binary load at all; they are not notarized, so first launch needs the step
described in the README.

## Layout

```
main.go                 window, service registration
internal/
  app/                  Wails services — the frontend's whole API surface
  store/                SQLite: servers, projects, agents, memories, skills, runs
  sshx/                 connection pool, PTY manager, auth
  localx/               the same three things for this computer: exec, PTY, files
  tmuxx/                the tmux wrapper
  sftpx/                file transfer
  metrics/              one-command host vitals
  agentkit/             which agent CLIs exist and how to install them
  llm/                  Ollama client
  memory/               vector storage, redaction, retrieval
  skill/                skill lifecycle, versions, matching
  orch/                 the orchestrator loop, tool registry, gate
    catalog/            what tools exist and how dangerous each one is
frontend/src/           React UI
```

Three rules hold across the Go side:

- **Secrets never cross into the frontend.** It learns whether a password is set,
  never what it is.
- **The orchestrator reaches nothing a person could not.** Its tools are thin
  wrappers over the same services the UI calls, with a gate in front.
- **Nothing long-lived belongs to this process.** Agents live in remote tmux;
  the app is a viewer and a controller.
