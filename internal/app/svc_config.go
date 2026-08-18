package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"agentmux/internal/portable"
)

// ConfigService carries an installation between machines.
//
// The file it writes is the only path by which a secret leaves this computer,
// so the passphrase is required rather than optional and the export is
// deliberately an explicit act: nothing here runs on a schedule, syncs, or
// phones anywhere. The file goes where the person puts it.
type ConfigService struct{ core *Core }

// NewConfigService binds a configuration service to the core.
func NewConfigService(c *Core) *ConfigService { return &ConfigService{core: c} }

// ServiceName identifies the service in Wails logs.
func (c *ConfigService) ServiceName() string { return "ConfigService" }

// SuggestFilename is the name the save dialog opens with — dated, because the
// second thing anyone wants to know about a config file is when it was made.
func (c *ConfigService) SuggestFilename() string {
	return fmt.Sprintf("agentmux-%s.agentmux", time.Now().Format("2006-01-02"))
}

// Export writes the configuration to path, encrypted under the passphrase, and
// returns what went into it.
func (c *ConfigService) Export(path, passphrase string, opt portable.Options) (portable.Manifest, error) {
	if strings.TrimSpace(path) == "" {
		return portable.Manifest{}, fmt.Errorf("no file was chosen")
	}
	if err := portable.CheckPassphrase(passphrase); err != nil {
		return portable.Manifest{}, err
	}
	bundle, err := portable.Build(c.core.Store, opt)
	if err != nil {
		return portable.Manifest{}, err
	}
	if err := portable.Write(path, bundle, passphrase); err != nil {
		return portable.Manifest{}, err
	}
	return bundle.Manifest(), nil
}

// Inspect opens a file and reports what is in it without changing anything.
//
// It exists so the import can be answered rather than guessed at: a file's
// contents are encrypted, so the only way to say "this holds four hosts and
// nine projects" before importing is to open it and count.
func (c *ConfigService) Inspect(path, passphrase string) (portable.Manifest, error) {
	bundle, err := portable.Read(path, passphrase)
	if err != nil {
		return portable.Manifest{}, err
	}
	return bundle.Manifest(), nil
}

// Import merges a file into this installation, adding what is missing and
// leaving everything that is already here exactly as it is.
func (c *ConfigService) Import(path, passphrase string) (portable.Result, error) {
	bundle, err := portable.Read(path, passphrase)
	if err != nil {
		return portable.Result{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Agents that arrive without a usable session are named the way this
	// machine names its own, rather than by a second copy of the convention
	// living in the import.
	agents := NewAgentService(c.core)
	res, err := portable.Apply(ctx, c.core.Store, c.core.Skills, bundle,
		func(workspaceID, agentName string) string {
			name, err := agents.SuggestSession(workspaceID, agentName)
			if err != nil {
				return ""
			}
			return name
		})
	if err != nil {
		return res, err
	}
	// Every window is showing a tree that is now out of date, including the
	// detached ones that never ask for it.
	if list, lerr := c.core.Store.ListAgents(); lerr == nil {
		c.core.Emit("agents:updated", list)
	}
	return res, nil
}

// FileInfo is the little that can be said about a file before a passphrase is
// typed: that it is one of ours, and when it was written.
type FileInfo struct {
	Recognised bool  `json:"recognised"`
	ExportedAt int64 `json:"exportedAt"`
}

// Peek reads the unencrypted header, so a picker can tell someone they chose
// the wrong file without asking them for a passphrase first.
func (c *ConfigService) Peek(path string) (FileInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return FileInfo{}, err
	}
	at := portable.ExportedAt(raw)
	return FileInfo{Recognised: at != 0, ExportedAt: at}, nil
}
