// Package app wires the store, SSH pool, tmux client and PTY manager together
// and exposes them to the frontend as Wails services.
package app

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"agentmux/internal/llm"
	"agentmux/internal/localx"
	"agentmux/internal/memory"
	"agentmux/internal/natmux"
	"agentmux/internal/orch"
	"agentmux/internal/sftpx"
	"agentmux/internal/skill"
	"agentmux/internal/sshx"
	"agentmux/internal/store"
	"agentmux/internal/tmuxx"
	"agentmux/internal/winhost"
)

// idleTTL closes SSH connections that nothing has used for this long. It is the
// mechanism that keeps a hundred configured servers from meaning a hundred live
// sockets; anything a user is actually watching holds a lease and is exempt.
const idleTTL = 10 * time.Minute

// Core holds the shared, long-lived state of the application.
type Core struct {
	Store  *store.Store
	Pool   *sshx.Pool
	Tmux   Mux
	Shells *sshx.ShellManager
	Files  *sftpx.Client
	Memory *memory.Index
	Skills *skill.Manager
	Orch   *orch.Engine

	// The machine AgentMux is running on, managed as a host in its own right.
	// Local  runs its commands, LocalFiles reads and writes its filesystem, and
	// Run is what everything above uses so it never has to know which is which.
	Local      *localx.Runner
	LocalFiles *localx.Files
	Run        Runner

	// The same machine's native Windows side, when there is one: PowerShell,
	// Windows paths, Windows toolchains — and sessions that persist through
	// AgentMux's own daemon instead of tmux, because some work (MSVC, WPF,
	// running the .exe that was just built) cannot happen inside WSL.
	NativeLocal *localx.NativeRunner
	NativeFiles *localx.Files
	NatMux      *natmux.Client

	// Session daemon clients for remote Windows hosts, one per server, built on
	// first use. Each speaks the same protocol as NatMux, carried through an
	// SSH port forward instead of the local pipe.
	remoteMuxMu sync.Mutex
	remoteMux   map[string]*natmux.Client

	// Which kernel each host runs, asked once and remembered. A machine does
	// not change operating system while AgentMux is looking at it, and the
	// question is asked on every metrics poll.
	unameMu sync.Mutex
	uname   map[string]string

	// llmMu guards the client, which is rebuilt whenever the user points
	// AgentMux at a different Ollama.
	llmMu sync.RWMutex
	llm   *llm.Client

	emitMu sync.RWMutex
	emitFn func(name string, data any)

	stopOnce sync.Once
	stopCh   chan struct{}
}

// NewCore opens the database and builds the connection machinery.
func NewCore() (*Core, error) {
	st, err := store.Open()
	if err != nil {
		return nil, err
	}

	c := &Core{Store: st, stopCh: make(chan struct{})}
	c.Pool = sshx.NewPool(c, idleTTL, func(s sshx.ConnState) {
		c.Emit("server:state", s)
	})
	c.Local = localx.NewRunner()
	c.LocalFiles = localx.NewFiles(c.Local)
	c.NativeLocal = localx.NewNativeRunner()
	c.NativeFiles = localx.NewNativeFiles(c.NativeLocal)
	c.NatMux = natmux.NewClient()
	c.Run = hostRunner{core: c}
	// tmux, agent detection and host metrics all reach a host through Run, so a
	// local host gets the same treatment as a remote one without any of them
	// carrying a special case. Session operations route the same way: tmux for
	// every host that has one, the native session daemon for the one kind that
	// cannot.
	c.Tmux = muxRouter{core: c, tmux: tmuxx.New(c.Run)}
	c.Shells = sshx.NewShellManager(c.Pool, c.Emit)
	localTerminals := localx.NewTerminals(c.Local)
	nativeTerminals := localx.NewNativeTerminals(c.NativeLocal)
	c.Shells.UseLocal(func(serverID string) sshx.Opener {
		switch c.Store.ServerKindOf(serverID) {
		case store.KindLocal:
			return localTerminals
		case store.KindLocalWin:
			return nativeTerminals
		}
		return nil
	})
	c.Files = sftpx.New(c.Pool, c.Emit)

	// Nothing here reaches out to Ollama. Building the client is local work, and
	// the memory library has to be usable — browsable, writable — on a machine
	// where no model runtime is installed at all.
	c.llm = llm.New(st.GetSetting(SettingLLMBaseURL, ""))
	c.Memory = memory.NewIndex(st, c.llm, func() string { return c.EmbedModel() })
	c.Skills = skill.NewManager(st, c.llm, func() string { return c.EmbedModel() })

	// A run only exists inside a running process, so anything the database
	// still calls "running" died with the last one.
	_ = st.RecoverRunningRuns()

	registry, err := c.buildRegistry()
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("tools: %w", err)
	}
	c.Orch = orch.New(orch.Options{
		Store: st, LLM: c.llm, Memory: c.Memory, Skills: c.Skills,
		Registry: registry, Observer: fleetObserver{core: c},
		Model: func() string { return c.ChatModel() },
		Emit:  c.Emit,
	})
	c.startPatrol()
	return c, nil
}

// Runner executes one command on a host. All transports satisfy it, and
// everything that runs commands takes it rather than any of them.
type Runner interface {
	Exec(serverID, cmd string) (sshx.ExecResult, error)
}

// hostRunner routes a command to the transport that owns the host.
type hostRunner struct{ core *Core }

// Exec runs the command where the host actually is. What "runs a command" means
// differs with it: a POSIX shell for SSH and local hosts, PowerShell for the
// Windows ones — callers that build command lines ask the kind first. For a
// remote Windows host the PowerShell script travels as -EncodedCommand, the one
// spelling that survives whichever default shell its sshd hands commands to.
func (r hostRunner) Exec(serverID, cmd string) (sshx.ExecResult, error) {
	switch r.core.Store.ServerKindOf(serverID) {
	case store.KindLocal:
		return r.core.Local.Exec(serverID, cmd)
	case store.KindLocalWin:
		return r.core.NativeLocal.Exec(serverID, cmd)
	case store.KindSSHWin:
		return r.core.Pool.Exec(serverID, sshx.PowerShellCommand(cmd))
	}
	return r.core.Pool.Exec(serverID, cmd)
}

// Mux is the session layer for one host: create, list, address, type into and
// capture persistent terminal sessions. tmux implements it everywhere tmux
// exists; the native session daemon implements it where it cannot.
type Mux interface {
	Available(serverID string) tmuxx.Info
	ListSessions(serverID string) ([]tmuxx.Session, error)
	ListPanes(serverID string) ([]tmuxx.Pane, error)
	HasSession(serverID, name string) (bool, error)
	NewSession(serverID, name, cwd string) error
	KillSession(serverID, name string) error
	RenameSession(serverID, from, to string) error
	SendText(serverID, tgt, text string, pressEnter bool) error
	SendKey(serverID, tgt, key string) error
	CapturePane(serverID, tgt string, lines int) (string, error)
}

// muxRouter sends session operations to whichever layer owns the host.
type muxRouter struct {
	core *Core
	tmux *tmuxx.Client
}

func (m muxRouter) of(serverID string) Mux {
	if m.core.IsWinHost(serverID) {
		return m.core.NatMuxFor(serverID)
	}
	return m.tmux
}

func (m muxRouter) Available(serverID string) tmuxx.Info { return m.of(serverID).Available(serverID) }
func (m muxRouter) ListSessions(serverID string) ([]tmuxx.Session, error) {
	return m.of(serverID).ListSessions(serverID)
}
func (m muxRouter) ListPanes(serverID string) ([]tmuxx.Pane, error) {
	return m.of(serverID).ListPanes(serverID)
}
func (m muxRouter) HasSession(serverID, name string) (bool, error) {
	return m.of(serverID).HasSession(serverID, name)
}
func (m muxRouter) NewSession(serverID, name, cwd string) error {
	return m.of(serverID).NewSession(serverID, name, cwd)
}
func (m muxRouter) KillSession(serverID, name string) error {
	return m.of(serverID).KillSession(serverID, name)
}
func (m muxRouter) RenameSession(serverID, from, to string) error {
	return m.of(serverID).RenameSession(serverID, from, to)
}
func (m muxRouter) SendText(serverID, tgt, text string, pressEnter bool) error {
	return m.of(serverID).SendText(serverID, tgt, text, pressEnter)
}
func (m muxRouter) SendKey(serverID, tgt, key string) error {
	return m.of(serverID).SendKey(serverID, tgt, key)
}
func (m muxRouter) CapturePane(serverID, tgt string, lines int) (string, error) {
	return m.of(serverID).CapturePane(serverID, tgt, lines)
}

// IsLocal reports whether a host is this computer's POSIX side. It reads one
// column, and is asked before every command, terminal and file operation.
func (c *Core) IsLocal(serverID string) bool {
	return c.Store.ServerKindOf(serverID) == store.KindLocal
}

// IsLocalWin reports whether a host is this computer's native Windows side.
func (c *Core) IsLocalWin(serverID string) bool {
	return c.Store.ServerKindOf(serverID) == store.KindLocalWin
}

// IsLocalAny reports whether a host is this computer in either flavour.
func (c *Core) IsLocalAny(serverID string) bool {
	kind := c.Store.ServerKindOf(serverID)
	return kind == store.KindLocal || kind == store.KindLocalWin
}

// IsSSHWin reports whether a host is a remote Windows machine.
func (c *Core) IsSSHWin(serverID string) bool {
	return c.Store.ServerKindOf(serverID) == store.KindSSHWin
}

// IsWinHost reports whether a host speaks PowerShell — this computer's native
// side or a remote Windows machine. It is the question every dialect choice
// asks: which shell a command line is composed for, which probe to run, which
// session layer holds the agents.
func (c *Core) IsWinHost(serverID string) bool {
	kind := c.Store.ServerKindOf(serverID)
	return kind == store.KindLocalWin || kind == store.KindSSHWin
}

// IsDarwinHost reports whether a host is a Mac — this computer when AgentMux
// runs on macOS, or a Mac reached over SSH.
//
// macOS is a POSIX host in every way that matters to shells and tmux, so it
// has no kind of its own; where it differs is in what it can be asked about
// itself, which is /proc on Linux and sysctl here. The local answer is free.
// A remote one costs one `uname -s`, kept for the session.
func (c *Core) IsDarwinHost(serverID string) bool {
	kind := c.Store.ServerKindOf(serverID)
	if kind == store.KindLocalWin || kind == store.KindSSHWin {
		return false
	}
	if kind == store.KindLocal {
		return runtime.GOOS == "darwin"
	}
	return c.unameOf(serverID) == "Darwin"
}

// unameOf reports a remote host's kernel name, asking it at most once. A failed
// probe is not remembered: a server that was offline is asked again rather than
// being treated as something it is not for the rest of the session.
func (c *Core) unameOf(serverID string) string {
	c.unameMu.Lock()
	if c.uname == nil {
		c.uname = map[string]string{}
	}
	if v, ok := c.uname[serverID]; ok {
		c.unameMu.Unlock()
		return v
	}
	c.unameMu.Unlock()

	res, err := c.Run.Exec(serverID, "uname -s 2>/dev/null")
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(res.Stdout)
	if name == "" {
		return ""
	}
	c.unameMu.Lock()
	c.uname[serverID] = name
	c.unameMu.Unlock()
	return name
}

// NatMuxFor returns the session daemon client for a Windows host: the local
// one over its pipe, or a per-server client carried through an SSH forward.
func (c *Core) NatMuxFor(serverID string) *natmux.Client {
	if !c.IsSSHWin(serverID) {
		return c.NatMux
	}
	c.remoteMuxMu.Lock()
	defer c.remoteMuxMu.Unlock()
	if c.remoteMux == nil {
		c.remoteMux = map[string]*natmux.Client{}
	}
	if cl, ok := c.remoteMux[serverID]; ok {
		return cl
	}
	cl := natmux.NewRemoteClient(&winhost.Transport{
		ServerID: serverID,
		Pool:     c.Pool,
		Username: func() (string, error) {
			s, err := c.Store.GetServer(serverID)
			return s.Username, err
		},
		LocalExe: winDeployExe,
	})
	c.remoteMux[serverID] = cl
	return cl
}

// winDeployExe locates the Windows AgentMux binary to install on a remote
// Windows host. A Windows build deploys itself; any other build needs to be
// told where a Windows build is.
func winDeployExe() (string, error) {
	if runtime.GOOS == "windows" {
		return os.Executable()
	}
	if p := os.Getenv("AGENTMUX_WIN_EXE"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("AGENTMUX_WIN_EXE points at %s, which does not exist", p)
	}
	return "", errors.New(
		"deploying the session daemon to a remote Windows host needs a Windows build of AgentMux. " +
			"Build one (GOOS=windows go build) and set AGENTMUX_WIN_EXE to its path, " +
			"or copy agentmux.exe to the host as %LOCALAPPDATA%\\AgentMux\\agentmux-host.exe and run it once with --natmuxd")
}

// IsReachable reports whether work can be done on a host right now.
//
// For a remote host that means a live pooled connection — the test the poller and
// the fleet view have always used, so that a hundred configured servers do not
// mean a hundred sockets. This computer is always reachable: there is nothing to
// connect, which would otherwise make it the one host whose agents never got
// polled.
func (c *Core) IsReachable(serverID string) bool {
	return c.IsLocalAny(serverID) || c.Pool.IsConnected(serverID)
}

// Settings keys for the local model runtime.
const (
	SettingLLMBaseURL    = "llm.baseUrl"
	SettingLLMChatModel  = "llm.chatModel"
	SettingLLMEmbedModel = "llm.embedModel"
)

// LLM returns the current client.
func (c *Core) LLM() *llm.Client {
	c.llmMu.RLock()
	defer c.llmMu.RUnlock()
	return c.llm
}

// EmbedModel is the configured embedding model.
func (c *Core) EmbedModel() string {
	return c.Store.GetSetting(SettingLLMEmbedModel, llm.DefaultEmbedModel)
}

// ChatModel is the configured planning model.
func (c *Core) ChatModel() string {
	return c.Store.GetSetting(SettingLLMChatModel, llm.DefaultChatModel)
}

// SetLLMBaseURL points AgentMux at a different Ollama and rebuilds the client.
func (c *Core) SetLLMBaseURL(baseURL string) error {
	if err := c.Store.SetSetting(SettingLLMBaseURL, baseURL); err != nil {
		return err
	}
	client := llm.New(baseURL)

	c.llmMu.Lock()
	c.llm = client
	c.llmMu.Unlock()

	c.Memory.SetEmbedder(client)
	c.Skills.SetEmbedder(client)
	return nil
}

// SetEmitter installs the Wails event emitter once the application exists.
func (c *Core) SetEmitter(fn func(name string, data any)) {
	c.emitMu.Lock()
	c.emitFn = fn
	c.emitMu.Unlock()
}

// Emit publishes an event to the frontend, dropping it if the UI is not up yet.
func (c *Core) Emit(name string, data any) {
	c.emitMu.RLock()
	fn := c.emitFn
	c.emitMu.RUnlock()
	if fn != nil {
		fn(name, data)
	}
}

// Shutdown tears everything down in dependency order.
func (c *Core) Shutdown() {
	c.stopOnce.Do(func() {
		close(c.stopCh)
		c.Shells.CloseAll()
		c.Files.Close()
		c.Pool.Stop()
		_ = c.Store.Close()
	})
}

// --- sshx.Resolver ----------------------------------------------------------

// Resolve turns a server id into connection credentials for the pool.
func (c *Core) Resolve(serverID string) (sshx.Target, error) {
	s, err := c.Store.GetServer(serverID)
	if err != nil {
		return sshx.Target{}, err
	}
	// The one guard that keeps the pool honest: a local host has no address, so
	// every SSH path that could be reached with one — a pooled connection, a
	// transfer, a probe — fails here with a sentence instead of dialling "".
	if s.Kind == store.KindLocal || s.Kind == store.KindLocalWin {
		return sshx.Target{}, fmt.Errorf(
			"%s is this computer, which is not reached over SSH", s.Name)
	}
	password, passphrase, err := c.Store.Secrets(serverID)
	if err != nil {
		return sshx.Target{}, err
	}
	t := sshx.Target{
		ServerID:   s.ID,
		Name:       s.Name,
		Host:       s.Host,
		Port:       s.Port,
		User:       s.Username,
		AuthType:   string(s.AuthType),
		KeyPath:    s.KeyPath,
		Password:   password,
		Passphrase: passphrase,
		HostKey:    s.HostKey,
	}
	if s.JumpServerID != nil {
		t.JumpServerID = *s.JumpServerID
	}
	return t, nil
}

// PinHostKey stores a trust-on-first-use host key.
func (c *Core) PinHostKey(serverID, hostKey string) error {
	return c.Store.PinHostKey(serverID, hostKey)
}

// MarkOK records a successful connection.
func (c *Core) MarkOK(serverID string) {
	_ = c.Store.MarkServerOK(serverID)
}

// --- helpers ----------------------------------------------------------------

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Slug normalises a name into something safe for a tmux session path segment.
func Slug(s string) string {
	out := slugRE.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "-")
	out = strings.Trim(out, "-")
	if out == "" {
		out = "untitled"
	}
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

// DefaultSessionName builds the conventional tmux session name for an agent.
// tmux treats ':' and '.' as target separators, so only '/' is used to nest.
func DefaultSessionName(projectName, agentName string) string {
	return fmt.Sprintf("agentmux/%s/%s", Slug(projectName), Slug(agentName))
}

// shellCommands are the process names that mean "the pane is sitting at a
// prompt" rather than "an agent is working". The Windows shells are here for
// native sessions, whose panes report PowerShell the way tmux panes report bash.
var shellCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
	"ksh": true, "csh": true, "tcsh": true, "ash": true, "login": true,
	"powershell": true, "pwsh": true, "cmd": true,
}
