package app

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"agentmux/internal/orch"
	"agentmux/internal/orch/catalog"
	"agentmux/internal/store"
)

// Settings for the orchestrator.
const (
	// SettingOrchEnabled gates the whole feature. It is off until someone turns
	// it on: a thing that acts on its own should not start acting because it
	// was installed.
	SettingOrchEnabled = "orch.enabled"
	// SettingOrchPatrolMinutes is how often it looks around by itself. Zero
	// means never.
	SettingOrchPatrolMinutes = "orch.patrolMinutes"
)

// OrchService exposes the orchestrator to the frontend.
type OrchService struct{ core *Core }

// NewOrchService binds an orchestrator service to the core.
func NewOrchService(c *Core) *OrchService { return &OrchService{core: c} }

// ServiceName identifies the service in Wails logs.
func (o *OrchService) ServiceName() string { return "OrchService" }

// OrchConfig is the user-facing orchestrator configuration.
type OrchConfig struct {
	Enabled       bool `json:"enabled"`
	PatrolMinutes int  `json:"patrolMinutes"`
}

// Config returns the current settings.
func (o *OrchService) Config() OrchConfig {
	minutes, _ := strconv.Atoi(o.core.Store.GetSetting(SettingOrchPatrolMinutes, "0"))
	return OrchConfig{
		Enabled:       o.core.Store.GetSetting(SettingOrchEnabled, "false") == "true",
		PatrolMinutes: minutes,
	}
}

// SaveConfig persists the settings.
func (o *OrchService) SaveConfig(cfg OrchConfig) (OrchConfig, error) {
	if cfg.PatrolMinutes < 0 {
		cfg.PatrolMinutes = 0
	}
	if cfg.PatrolMinutes > 0 && cfg.PatrolMinutes < 5 {
		// More often than this is a way to burn a laptop's battery watching a
		// fleet that changes on a scale of minutes at best.
		cfg.PatrolMinutes = 5
	}
	if err := o.core.Store.SetSetting(SettingOrchEnabled, boolText(cfg.Enabled)); err != nil {
		return OrchConfig{}, err
	}
	if err := o.core.Store.SetSetting(
		SettingOrchPatrolMinutes, strconv.Itoa(cfg.PatrolMinutes)); err != nil {
		return OrchConfig{}, err
	}
	return o.Config(), nil
}

// Status is what the panel renders at the top.
type Status struct {
	Enabled       bool               `json:"enabled"`
	Running       bool               `json:"running"`
	RunID         string             `json:"runId"`
	PatrolMinutes int                `json:"patrolMinutes"`
	Pending       []store.Approval   `json:"pending"`
	Tools         []catalog.ToolMeta `json:"tools"`
}

// Status reports what the orchestrator is doing.
func (o *OrchService) Status() (Status, error) {
	cfg := o.Config()
	runID, running := o.core.Orch.Busy()
	pending, err := o.core.Store.PendingApprovals()
	if err != nil {
		return Status{}, err
	}
	return Status{
		Enabled: cfg.Enabled, Running: running, RunID: runID,
		PatrolMinutes: cfg.PatrolMinutes, Pending: pending, Tools: catalog.All(),
	}, nil
}

// Start asks the orchestrator to work towards a goal.
func (o *OrchService) Start(goal, projectID string) (store.Run, error) {
	if !o.Config().Enabled {
		return store.Run{}, errors.New("the orchestrator is switched off — turn it on in Settings")
	}
	if strings.TrimSpace(goal) == "" {
		return store.Run{}, errors.New("say what you want done")
	}
	return o.core.Orch.Start(orch.Request{
		Goal: goal, ProjectID: projectID, Trigger: orch.TriggerHuman,
	})
}

// Stop cancels the run in progress.
func (o *OrchService) Stop() { o.core.Orch.Stop() }

// Decide answers a pending approval.
func (o *OrchService) Decide(id string, approved bool, note string) error {
	return o.core.Orch.Decide(id, approved, note)
}

// Runs lists recent runs, newest first.
func (o *OrchService) Runs(limit int) ([]store.Run, error) { return o.core.Store.ListRuns(limit) }

// Steps returns the decision log of one run.
func (o *OrchService) Steps(runID string) ([]store.Step, error) {
	return o.core.Store.ListSteps(runID)
}

// Pending lists approvals waiting for an answer.
func (o *OrchService) Pending() ([]store.Approval, error) { return o.core.Store.PendingApprovals() }

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// startPatrol runs the scheduled look-around.
//
// A patrol is deliberately not the same thing as a run someone asked for: it
// carries the schedule trigger, which the gate refuses every non-read tool for,
// whatever any server's trust level says. It can notice that an agent has been
// stuck for an hour and write that down. It cannot restart it.
func (c *Core) startPatrol() {
	go func() {
		// A short tick with the real interval checked inside means changing the
		// setting takes effect without a restart.
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		var last time.Time
		for {
			select {
			case <-c.stopCh:
				return
			case <-ticker.C:
				enabled := c.Store.GetSetting(SettingOrchEnabled, "false") == "true"
				minutes, _ := strconv.Atoi(c.Store.GetSetting(SettingOrchPatrolMinutes, "0"))
				if !enabled || minutes <= 0 {
					continue
				}
				if time.Since(last) < time.Duration(minutes)*time.Minute {
					continue
				}
				if _, busy := c.Orch.Busy(); busy {
					continue
				}
				last = time.Now()
				_, _ = c.Orch.Start(orch.Request{
					Goal: "Look over the fleet. Report any agent that is stuck, errored or idle " +
						"when it should not be, and anything that looks like it needs attention. " +
						"Do not change anything.",
					Trigger: orch.TriggerSchedule,
				})
			}
		}
	}()
}
