package app

import (
	"context"
	"strings"
	"time"

	"agentmux/internal/llm"
)

// LLMService configures and inspects the local model runtime.
//
// It is deliberately thin. AgentMux does not manage Ollama — it does not start
// it, install models or hold its hand; it reports what is there and says what
// to run when something is missing.
type LLMService struct{ core *Core }

// NewLLMService binds a runtime service to the core.
func NewLLMService(c *Core) *LLMService { return &LLMService{core: c} }

// ServiceName identifies the service in Wails logs.
func (l *LLMService) ServiceName() string { return "LLMService" }

// Config is the user-facing runtime configuration.
type Config struct {
	BaseURL    string `json:"baseUrl"`
	ChatModel  string `json:"chatModel"`
	EmbedModel string `json:"embedModel"`
}

// Config returns the current settings, with defaults filled in.
func (l *LLMService) Config() Config {
	return Config{
		BaseURL:    l.core.Store.GetSetting(SettingLLMBaseURL, llm.DefaultBaseURL),
		ChatModel:  l.core.ChatModel(),
		EmbedModel: l.core.EmbedModel(),
	}
}

// SaveConfig persists the settings and rebuilds the client.
//
// Changing the embedding model invalidates every stored vector: numbers from
// two models describe different spaces, and comparing across them returns
// confident nonsense rather than an error. So the cache is dropped here and the
// UI is told the library needs rebuilding.
func (l *LLMService) SaveConfig(cfg Config) (llm.Status, error) {
	cfg.BaseURL = strings.TrimSpace(cfg.BaseURL)
	cfg.ChatModel = strings.TrimSpace(cfg.ChatModel)
	cfg.EmbedModel = strings.TrimSpace(cfg.EmbedModel)

	previousEmbed := l.core.EmbedModel()

	if err := l.core.SetLLMBaseURL(cfg.BaseURL); err != nil {
		return llm.Status{}, err
	}
	if err := l.core.Store.SetSetting(SettingLLMChatModel, cfg.ChatModel); err != nil {
		return llm.Status{}, err
	}
	if err := l.core.Store.SetSetting(SettingLLMEmbedModel, cfg.EmbedModel); err != nil {
		return llm.Status{}, err
	}

	if cfg.EmbedModel != previousEmbed {
		l.core.Memory.Invalidate()
		if stats, err := l.core.Memory.Stats(); err == nil {
			l.core.Emit("memory:stats", stats)
		}
	}
	return l.Status(), nil
}

// Status probes the runtime. It never returns an error: "not installed" is a
// state to render, not a failure.
func (l *LLMService) Status() llm.Status {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return l.core.LLM().Probe(ctx, l.core.ChatModel(), l.core.EmbedModel())
}

// Models lists what is installed, for the model pickers.
func (l *LLMService) Models() ([]llm.Model, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return l.core.LLM().Models(ctx)
}
