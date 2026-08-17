// Package app wires the store, SSH pool, tmux client and PTY manager together
// and exposes them to the frontend as Wails services.
package app

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"agentmux/internal/llm"
	"agentmux/internal/memory"
	"agentmux/internal/orch"
	"agentmux/internal/sftpx"
	"agentmux/internal/skill"
	"agentmux/internal/sshx"
	"agentmux/internal/store"
	"agentmux/internal/tmuxx"
)

// idleTTL closes SSH connections that nothing has used for this long. It is the
// mechanism that keeps a hundred configured servers from meaning a hundred live
// sockets; anything a user is actually watching holds a lease and is exempt.
const idleTTL = 10 * time.Minute

// Core holds the shared, long-lived state of the application.
type Core struct {
	Store  *store.Store
	Pool   *sshx.Pool
	Tmux   *tmuxx.Client
	Shells *sshx.ShellManager
	Files  *sftpx.Client
	Memory *memory.Index
	Skills *skill.Manager
	Orch   *orch.Engine

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
	c.Tmux = tmuxx.New(c.Pool)
	c.Shells = sshx.NewShellManager(c.Pool, c.Emit)
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
// prompt" rather than "an agent is working".
var shellCommands = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
	"ksh": true, "csh": true, "tcsh": true, "ash": true, "login": true,
}
