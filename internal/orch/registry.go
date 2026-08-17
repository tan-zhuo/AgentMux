package orch

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"agentmux/internal/orch/catalog"
	"agentmux/internal/store"
)

// Risk and ToolMeta are the catalogue's, re-exported so that callers of the
// orchestrator do not have to know the catalogue is a separate package.
type (
	Risk     = catalog.Risk
	ToolMeta = catalog.ToolMeta
)

const (
	RiskRead        = catalog.RiskRead
	RiskAct         = catalog.RiskAct
	RiskDestructive = catalog.RiskDestructive
)

// Trigger is what started a run.
type Trigger string

const (
	// TriggerHuman is a person asking for something.
	TriggerHuman Trigger = "human"
	// TriggerSchedule is the orchestrator looking around by itself.
	//
	// Unattended, holding SSH tools, reading text a remote machine produced —
	// those three together are the dangerous combination. This trigger removes
	// the middle one: a scheduled run can look and report, and that is all.
	TriggerSchedule Trigger = "schedule"
)

// Tool is a catalogue entry bound to something that actually runs.
type Tool struct {
	Name string
	// Schema is the JSON Schema for the arguments, handed to the model.
	Schema json.RawMessage
	// Invoke is unexported from the engine's point of view: it is only ever
	// reached through Registry.Call, which will not run anything the gate has
	// not cleared.
	Invoke func(ctx context.Context, args json.RawMessage) (any, error)
}

// Registry binds tool names to implementations.
//
// A tool's description and risk are not settable here; they come from the
// catalogue. Registering something that is not in the catalogue, or trying to
// give it a different risk, is a programming error rather than a configuration
// option — which is what stops a tool from being quietly declassified.
type Registry struct {
	tools map[string]Tool
	// trust answers "how much is this server trusted?". It is a function
	// rather than a value because the answer changes while the app is running.
	trust func(serverID, agentID string) store.TrustLevel
}

// NewRegistry builds an empty registry.
func NewRegistry(trust func(serverID, agentID string) store.TrustLevel) *Registry {
	if trust == nil {
		trust = func(string, string) store.TrustLevel { return store.TrustNormal }
	}
	return &Registry{tools: map[string]Tool{}, trust: trust}
}

// Register adds an implementation for a catalogued tool.
func (r *Registry) Register(t Tool) error {
	if _, ok := catalog.Lookup(t.Name); !ok {
		return fmt.Errorf("%q is not in the tool catalogue", t.Name)
	}
	if t.Invoke == nil {
		return fmt.Errorf("%q has no implementation", t.Name)
	}
	if _, dup := r.tools[t.Name]; dup {
		return fmt.Errorf("%q is registered twice", t.Name)
	}
	r.tools[t.Name] = t
	return nil
}

// Names lists what is registered, in catalogue order.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tools))
	for name := range r.tools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Available returns the tools a run with this trigger may see.
//
// A scheduled run is not shown the tools it would be refused: offering a model
// something it cannot have wastes a turn and invites it to argue.
func (r *Registry) Available(trig Trigger) []ToolMeta {
	out := []ToolMeta{}
	for _, meta := range catalog.All() {
		if _, ok := r.tools[meta.Name]; !ok {
			continue
		}
		if trig == TriggerSchedule && meta.Risk != RiskRead {
			continue
		}
		out = append(out, meta)
	}
	return out
}

// Verdict is what the gate decided.
type Verdict string

const (
	// Allow runs it now.
	Allow Verdict = "allow"
	// Ask puts it in front of a person first.
	Ask Verdict = "ask"
	// Deny refuses outright, with no way to override in this run.
	Deny Verdict = "deny"
)

// Decision is a verdict with the reason for it, which is what the log and the
// approval card both show.
type Decision struct {
	Verdict Verdict
	Risk    Risk
	Reason  string
	// ServerID is the machine the call would touch, resolved from the
	// arguments, so the approval card can name it.
	ServerID string
}

// target is the shape every tool's arguments share when they name something.
// The gate reads only this much of them.
type target struct {
	ServerID string `json:"serverId"`
	AgentID  string `json:"agentId"`
}

// Gate decides whether a call happens. It is the only path to Invoke.
//
// The rules, in the order they apply:
//
//   - An unknown tool is refused. The model invents names when it is stuck.
//   - A scheduled run may only read. This is not configurable: the whole point
//     of the schedule trigger is that nobody is watching.
//   - Destructive tools always ask, on every server, however trusted.
//   - Otherwise the server's trust level decides.
func (r *Registry) Gate(name string, trig Trigger, args json.RawMessage) Decision {
	meta, ok := catalog.Lookup(name)
	if !ok {
		return Decision{Verdict: Deny, Reason: fmt.Sprintf("%q is not a tool AgentMux has", name)}
	}
	if _, registered := r.tools[name]; !registered {
		return Decision{Verdict: Deny, Risk: meta.Risk,
			Reason: fmt.Sprintf("%q is catalogued but not available in this build", name)}
	}

	var t target
	_ = json.Unmarshal(args, &t)
	trust := r.trust(t.ServerID, t.AgentID)
	d := Decision{Risk: meta.Risk, ServerID: t.ServerID}

	if trig == TriggerSchedule && meta.Risk != RiskRead {
		d.Verdict = Deny
		d.Reason = "a scheduled run may only look, never act — ask for this yourself if you want it"
		return d
	}

	switch meta.Risk {
	case RiskRead:
		d.Verdict = Allow
		d.Reason = "reading changes nothing"
	case RiskDestructive:
		d.Verdict = Ask
		d.Reason = "destructive actions are always confirmed, on every server"
	default: // RiskAct
		switch trust {
		case store.TrustTrusted:
			d.Verdict = Allow
			d.Reason = "this server is marked trusted for recoverable actions"
		case store.TrustProduction:
			d.Verdict = Ask
			d.Reason = "this server is marked production — everything but reading is confirmed"
		default:
			d.Verdict = Ask
			d.Reason = "this server asks before anything is changed"
		}
	}
	return d
}

// Call runs a tool that the gate has cleared.
//
// It takes the decision rather than recomputing it, so that a caller cannot
// invoke without having gone through the gate — there is no second path.
func (r *Registry) Call(
	ctx context.Context, name string, args json.RawMessage, d Decision,
) (any, error) {
	if d.Verdict == Deny {
		return nil, fmt.Errorf("refused: %s", d.Reason)
	}
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("%q is not available", name)
	}
	return t.Invoke(ctx, args)
}

// Definitions renders the available tools in the shape the chat API expects.
func (r *Registry) Definitions(trig Trigger) []toolDefinition {
	metas := r.Available(trig)
	out := make([]toolDefinition, 0, len(metas))
	for _, m := range metas {
		schema := r.tools[m.Name].Schema
		if len(schema) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, toolDefinition{Name: m.Name, Description: m.Description, Schema: schema})
	}
	return out
}

// toolDefinition is the intermediate shape; the engine converts it for the
// specific client so that this package does not depend on one chat API.
type toolDefinition struct {
	Name        string
	Description string
	Schema      json.RawMessage
}
