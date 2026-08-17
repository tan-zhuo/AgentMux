// Package orch runs the orchestrator: it observes, retrieves what is known,
// asks a local model what to do, and carries out whatever it is allowed to.
//
// The shape of this package is set by one fact. The text it reads comes from
// remote machines running AI agents, and the tools it holds can reach those
// machines over SSH. So the loop is built to be stoppable, refusable and
// legible: every proposal is written down before it happens, anything that
// changes state passes a gate, and nothing about a run is inferred later from
// what survived — it is all recorded as it goes.
package orch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"agentmux/internal/llm"
	"agentmux/internal/memory"
	"agentmux/internal/skill"
	"agentmux/internal/store"
)

// Limits. Each one exists because of a way a loop like this goes wrong.
const (
	// MaxSteps stops a model that is happily working forever.
	MaxSteps = 20
	// MaxWallClock bounds one run. Time spent waiting for a person does not
	// count against it: the limit is there to catch a runaway loop, and
	// punishing someone for taking a minute to read an approval card would
	// only teach them to approve faster.
	MaxWallClock = 15 * time.Minute
	// ApprovalTimeout is how long a request waits before it lapses.
	ApprovalTimeout = 30 * time.Minute
	// RepeatLimit catches the classic stuck model: the same call, the same
	// arguments, over and over, each time expecting a different answer.
	RepeatLimit = 3
	// MaxResultChars keeps one enormous log from crowding out the context.
	MaxResultChars = 4000
)

// Chatter is the part of the LLM client the engine needs.
type Chatter interface {
	Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error)
}

// Memories is the part of the memory index the engine needs.
type Memories interface {
	Search(ctx context.Context, q memory.Query) ([]memory.Hit, error)
	Put(ctx context.Context, m store.Memory) (memory.PutResult, error)
}

// Skills is the part of the skill manager the engine needs.
type Skills interface {
	Match(ctx context.Context, q skill.Query) ([]skill.Match, error)
	Create(ctx context.Context, sk store.Skill) (store.Skill, error)
	MarkUsed(id string) error
}

// Observer produces the "what is happening right now" section.
type Observer interface {
	Observe(ctx context.Context, projectID string) (string, error)
}

// Engine runs the loop.
type Engine struct {
	store  *store.Store
	llm    Chatter
	mem    Memories
	skills Skills
	reg    *Registry
	obs    Observer
	model  func() string
	emit   func(event string, data any)

	mu      sync.Mutex
	current *handle
	waiters map[string]chan store.ApprovalStatus
}

type handle struct {
	runID  string
	cancel context.CancelFunc
}

// Options collects the engine's dependencies.
type Options struct {
	Store    *store.Store
	LLM      Chatter
	Memory   Memories
	Skills   Skills
	Registry *Registry
	Observer Observer
	Model    func() string
	Emit     func(event string, data any)
}

// New builds an engine.
func New(o Options) *Engine {
	if o.Emit == nil {
		o.Emit = func(string, any) {}
	}
	return &Engine{
		store: o.Store, llm: o.LLM, mem: o.Memory, skills: o.Skills,
		reg: o.Registry, obs: o.Observer, model: o.Model, emit: o.Emit,
		waiters: map[string]chan store.ApprovalStatus{},
	}
}

// Request is a run to start.
type Request struct {
	Goal      string  `json:"goal"`
	ProjectID string  `json:"projectId"`
	Trigger   Trigger `json:"trigger"`
}

// Busy reports whether a run is in progress.
//
// Runs are serialised deliberately. Two loops acting on the same fleet at once
// would each be reasoning from a picture the other is invalidating, and the
// approval queue would stop telling anyone which run they were answering.
func (e *Engine) Busy() (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.current == nil {
		return "", false
	}
	return e.current.runID, true
}

// Start opens a run and works on it in the background.
func (e *Engine) Start(req Request) (store.Run, error) {
	if strings.TrimSpace(req.Goal) == "" {
		return store.Run{}, errors.New("a run needs a goal")
	}
	if req.Trigger == "" {
		req.Trigger = TriggerHuman
	}

	e.mu.Lock()
	if e.current != nil {
		id := e.current.runID
		e.mu.Unlock()
		return store.Run{}, fmt.Errorf("a run is already in progress (%s)", id)
	}

	run, err := e.store.CreateRun(store.Run{
		Goal: req.Goal, Trigger: string(req.Trigger),
		ProjectID: req.ProjectID, Model: e.model(),
	})
	if err != nil {
		e.mu.Unlock()
		return store.Run{}, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.current = &handle{runID: run.ID, cancel: cancel}
	e.mu.Unlock()

	e.emit("orch:run", run)
	go func() {
		defer func() {
			cancel()
			e.mu.Lock()
			e.current = nil
			e.mu.Unlock()
		}()
		e.execute(ctx, run, req)
	}()
	return run, nil
}

// Tools lists what a run with this trigger would be offered.
func (e *Engine) Tools(trig Trigger) []ToolMeta { return e.reg.Available(trig) }

// Explain runs the gate without running anything.
//
// It is the dry run: it answers "what would happen if this were proposed?"
// without proposing it, which is how a person can find out that a server is
// more trusted than they thought before something acts on that belief.
func (e *Engine) Explain(name string, trig Trigger, args json.RawMessage) Decision {
	return e.reg.Gate(name, trig, args)
}

// Stop cancels the run in progress.
func (e *Engine) Stop() {
	e.mu.Lock()
	h := e.current
	e.mu.Unlock()
	if h != nil {
		h.cancel()
	}
}

// Decide records a person's answer to a pending approval and releases the run.
func (e *Engine) Decide(id string, approved bool, note string) error {
	status := store.ApprovalRejected
	if approved {
		status = store.ApprovalApproved
	}
	if err := e.store.DecideApproval(id, status, note); err != nil {
		return err
	}

	e.mu.Lock()
	ch := e.waiters[id]
	delete(e.waiters, id)
	e.mu.Unlock()
	if ch != nil {
		ch <- status
	}

	a, err := e.store.GetApproval(id)
	if err == nil {
		e.emit("orch:approval", a)
	}
	return nil
}

// --- the loop ---------------------------------------------------------------

type runState struct {
	run      store.Run
	req      Request
	seq      int
	deadline time.Time
	repeats  map[string]int
	memoryID []string
	skillIDs []string
	steps    []store.Step
}

func (e *Engine) execute(ctx context.Context, run store.Run, req Request) {
	st := &runState{
		run: run, req: req,
		deadline: time.Now().Add(MaxWallClock),
		repeats:  map[string]int{},
	}

	messages, err := e.prepare(ctx, st)
	if err != nil {
		e.finish(st, store.RunFailed, "", err.Error())
		return
	}

	for {
		if err := ctx.Err(); err != nil {
			e.finish(st, store.RunCancelled, "", "stopped")
			return
		}
		if st.seq >= MaxSteps {
			e.finish(st, store.RunFailed, "",
				fmt.Sprintf("stopped after %d steps without finishing", MaxSteps))
			return
		}
		if time.Now().After(st.deadline) {
			e.finish(st, store.RunFailed, "",
				fmt.Sprintf("stopped after %s of work", MaxWallClock))
			return
		}

		resp, err := e.llm.Chat(ctx, llm.ChatRequest{
			Model:    e.model(),
			Messages: messages,
			Tools:    toolDefs(e.reg.Definitions(req.Trigger)),
		})
		if err != nil {
			e.finish(st, store.RunFailed, "", fmt.Sprintf("the model could not be reached: %v", err))
			return
		}
		messages = append(messages, resp.Message)

		if len(resp.Message.ToolCalls) == 0 {
			summary := strings.TrimSpace(resp.Message.Content)
			e.record(st, store.Step{Phase: "plan", Reasoning: summary, Outcome: "finished"})
			e.finish(st, store.RunSucceeded, summary, "")
			e.reflect(context.WithoutCancel(ctx), st, summary)
			return
		}

		// One call at a time. A model that asks for three things at once has
		// not seen the result of the first, so the second and third were
		// decided on a guess.
		call := resp.Message.ToolCalls[0]
		out, halt := e.handleCall(ctx, st, call, strings.TrimSpace(resp.Message.Content))
		messages = append(messages, llm.Message{
			Role: "tool", ToolName: call.Function.Name, Content: out,
		})
		if halt != "" {
			e.finish(st, store.RunFailed, "", halt)
			return
		}
	}
}

// prepare does the observe and retrieve phases and builds the opening context.
func (e *Engine) prepare(ctx context.Context, st *runState) ([]llm.Message, error) {
	observation := "Nothing could be observed."
	if e.obs != nil {
		raw, err := e.obs.Observe(ctx, st.req.ProjectID)
		if err == nil && strings.TrimSpace(raw) != "" {
			wrapped, werr := wrapObserved("the local fleet state", "", raw)
			if werr != nil {
				return nil, werr
			}
			observation = wrapped
		}
	}
	flagged, phrase := SuspectsInjection(observation)
	e.record(st, store.Step{
		Phase: "observe", Result: truncate(observation, MaxResultChars),
		InjectionFlag: flagged, Outcome: injectionNote(flagged, phrase),
	})

	data := planData{Goal: st.req.Goal, Observation: observation}

	// Retrieval is best-effort on purpose. With no model runtime there are no
	// embeddings and therefore no recall, but the loop can still observe, plan
	// and act — degrading to "works without memory" beats refusing to start.
	if e.mem != nil {
		if hits, err := e.mem.Search(ctx, memory.Query{
			Text: st.req.Goal, ProjectID: st.req.ProjectID, TopK: 6,
		}); err == nil {
			for _, h := range hits {
				data.Memories = append(data.Memories, promptMemory{
					Kind: string(h.Memory.Kind), Text: memoryLine(h.Memory),
				})
				st.memoryID = append(st.memoryID, h.Memory.ID)
			}
		}
	}
	if e.skills != nil {
		if matches, err := e.skills.Match(ctx, skill.Query{
			Text: st.req.Goal, ProjectID: st.req.ProjectID, TopK: 3,
		}); err == nil {
			for _, m := range matches {
				data.Skills = append(data.Skills, toPromptSkill(m))
				st.skillIDs = append(st.skillIDs, m.Skill.ID)
				_ = e.skills.MarkUsed(m.Skill.ID)
			}
		}
	}
	_ = e.store.SetRunSkills(st.run.ID, st.skillIDs)
	e.record(st, store.Step{
		Phase:     "retrieve",
		MemoryIDs: st.memoryID,
		Result: fmt.Sprintf("recalled %d memories, matched %d skills",
			len(data.Memories), len(data.Skills)),
		Outcome: "done",
	})

	system, err := render("plan.md", data)
	if err != nil {
		return nil, err
	}
	return []llm.Message{
		{Role: "system", Content: system},
		{Role: "user", Content: "Begin. Say what you are doing before each tool call."},
	}, nil
}

// handleCall gates one proposed tool call, runs it if allowed, and returns what
// to tell the model. A non-empty second return stops the run.
func (e *Engine) handleCall(
	ctx context.Context, st *runState, call llm.ToolCall, reasoning string,
) (string, string) {
	name := call.Function.Name
	args := call.Function.Arguments
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	started := time.Now()

	fingerprint := name + string(args)
	st.repeats[fingerprint]++
	if st.repeats[fingerprint] >= RepeatLimit {
		return "", fmt.Sprintf(
			"stopped: %s was called %d times with identical arguments", name, RepeatLimit)
	}

	decision := e.reg.Gate(name, st.req.Trigger, args)
	step := store.Step{
		Phase: "act", Tool: name, Args: string(args), Reasoning: reasoning,
		Risk: string(decision.Risk), SkillID: firstOrEmpty(st.skillIDs),
	}

	switch decision.Verdict {
	case Deny:
		step.Outcome = "denied"
		step.Result = decision.Reason
		step.DurationMs = time.Since(started).Milliseconds()
		e.record(st, step)
		return "This call was refused: " + decision.Reason, ""

	case Ask:
		approved, note, halted := e.await(ctx, st, name, args, reasoning, decision)
		if halted != "" {
			step.Outcome = "cancelled"
			step.Result = halted
			e.record(st, step)
			return "", halted
		}
		if !approved {
			step.Outcome = "rejected"
			step.Result = note
			step.DurationMs = time.Since(started).Milliseconds()
			e.record(st, step)
			return "A person refused this: " + note, ""
		}
	}

	result, err := e.reg.Call(ctx, name, args, decision)
	step.DurationMs = time.Since(started).Milliseconds()
	if err != nil {
		step.Outcome = "failed"
		step.Result = err.Error()
		e.record(st, step)
		return "The tool failed: " + err.Error(), ""
	}

	body := truncate(stringify(result), MaxResultChars)
	flagged, phrase := SuspectsInjection(body)
	step.Outcome = "done"
	step.Result = body
	step.InjectionFlag = flagged
	if flagged {
		step.Outcome = "done · " + injectionNote(true, phrase)
	}
	e.record(st, step)

	// Every result is wrapped, not only the ones from obviously remote tools.
	// Deciding per tool which output is trustworthy is a judgement that has to
	// be made again for every tool ever added, and getting it wrong once is
	// enough. Uniform treatment costs a few lines of context.
	wrapped, err := wrapObserved("tool "+name, decision.ServerID, body)
	if err != nil {
		return body, ""
	}
	return wrapped, ""
}

// await parks the run until a person decides, the request lapses, or the run is
// stopped. The wall-clock budget is extended by however long the wait took.
func (e *Engine) await(
	ctx context.Context, st *runState, name string, args json.RawMessage,
	reasoning string, d Decision,
) (approved bool, note string, halted string) {
	expires := time.Now().Add(ApprovalTimeout)
	flagged, _ := SuspectsInjection(reasoning)

	// The run is parked before the request exists, not after. The other order
	// leaves a window where something is queued for a person to answer while
	// the run still claims to be working, and anything reading both — the
	// panel, a test, the next person to look — sees a state that never made
	// sense.
	_ = e.store.UpdateRunStatus(st.run.ID, store.RunWaiting, "", "")
	e.emitRun(st)

	approval, err := e.store.CreateApproval(store.Approval{
		RunID: st.run.ID, Tool: name, Args: string(args), Risk: string(d.Risk),
		Rationale: reasoning, Target: d.ServerID, SkillID: firstOrEmpty(st.skillIDs),
		InjectionFlag: flagged, ExpiresAt: expires.Unix(),
	})
	if err != nil {
		_ = e.store.UpdateRunStatus(st.run.ID, store.RunRunning, "", "")
		return false, "", "the approval could not be recorded: " + err.Error()
	}

	ch := make(chan store.ApprovalStatus, 1)
	e.mu.Lock()
	e.waiters[approval.ID] = ch
	e.mu.Unlock()
	e.emit("orch:approval", approval)

	waitStart := time.Now()
	defer func() {
		st.deadline = st.deadline.Add(time.Since(waitStart))
		e.mu.Lock()
		delete(e.waiters, approval.ID)
		e.mu.Unlock()
		_ = e.store.UpdateRunStatus(st.run.ID, store.RunRunning, "", "")
		e.emitRun(st)
	}()

	select {
	case status := <-ch:
		latest, _ := e.store.GetApproval(approval.ID)
		return status == store.ApprovalApproved, latest.Note, ""

	case <-time.After(time.Until(expires)):
		// A lapsed request is recorded, never silently dropped: "nobody
		// answered" is something the log has to be able to show.
		_ = e.store.DecideApproval(approval.ID, store.ApprovalExpired, "nobody answered in time")
		if a, err := e.store.GetApproval(approval.ID); err == nil {
			e.emit("orch:approval", a)
		}
		return false, "nobody answered in time", ""

	case <-ctx.Done():
		_ = e.store.DecideApproval(approval.ID, store.ApprovalExpired, "the run was stopped")
		if a, err := e.store.GetApproval(approval.ID); err == nil {
			e.emit("orch:approval", a)
		}
		return false, "", "stopped while waiting for approval"
	}
}

// --- reflection -------------------------------------------------------------

// skillSchema constrains the reflection output. It is the same shape the skill
// editor writes, minus everything local to this machine.
const skillSchema = `{
  "type": "object",
  "required": ["name", "description", "trigger", "steps"],
  "properties": {
    "name":        {"type": "string", "minLength": 2, "maxLength": 60},
    "description": {"type": "string", "maxLength": 200},
    "trigger":     {"type": "string", "minLength": 8, "maxLength": 400},
    "steps": {
      "type": "array", "minItems": 1, "maxItems": 12,
      "items": {
        "type": "object",
        "required": ["order", "description"],
        "properties": {
          "order":            {"type": "integer", "minimum": 1},
          "description":      {"type": "string", "minLength": 4, "maxLength": 300},
          "recommendedTools": {"type": "array", "items": {"type": "string"}},
          "notes":            {"type": "string", "maxLength": 300}
        }
      }
    },
    "constraints": {"type": "array", "items": {"type": "string", "maxLength": 200}},
    "confidence":  {"type": "number", "minimum": 0, "maximum": 1}
  }
}`

// reflect asks whether anything here was worth keeping.
//
// It runs only after a run that succeeded and did at least three things: one or
// two steps is not a procedure, and generalising from a failure produces advice
// on how to fail again.
func (e *Engine) reflect(ctx context.Context, st *runState, outcome string) {
	acted := 0
	for _, s := range st.steps {
		if s.Phase == "act" && s.Outcome != "denied" {
			acted++
		}
	}
	if acted < 3 || e.skills == nil {
		return
	}

	data := reflectData{Goal: st.req.Goal, Outcome: outcome}
	for _, s := range st.steps {
		if s.Phase != "act" {
			continue
		}
		data.Steps = append(data.Steps, reflectStep{
			Seq: s.Seq, Tool: s.Tool, Reasoning: s.Reasoning, Result: truncate(s.Result, 300),
		})
	}
	prompt, err := render("reflect.md", data)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	resp, err := e.llm.Chat(ctx, llm.ChatRequest{
		Model:    e.model(),
		Messages: []llm.Message{{Role: "user", Content: prompt}},
		Format:   json.RawMessage(skillSchema),
	})
	if err != nil {
		return
	}

	content := strings.TrimSpace(resp.Message.Content)
	if content == "" || content == "null" {
		e.record(st, store.Step{Phase: "reflect", Outcome: "nothing worth keeping"})
		return
	}

	var draft struct {
		Name        string            `json:"name"`
		Description string            `json:"description"`
		Trigger     string            `json:"trigger"`
		Steps       []store.SkillStep `json:"steps"`
		Constraints []string          `json:"constraints"`
		Confidence  *float64          `json:"confidence"`
	}
	if err := json.Unmarshal([]byte(content), &draft); err != nil {
		e.record(st, store.Step{Phase: "reflect", Outcome: "the proposal could not be read",
			Result: truncate(content, 500)})
		return
	}
	if strings.TrimSpace(draft.Name) == "" || len(draft.Steps) == 0 {
		return
	}

	// Create() forces anything authored here into draft, so this cannot
	// activate a skill. The proposal has to survive a person before it can
	// shape another plan.
	created, err := e.skills.Create(ctx, store.Skill{
		Name: draft.Name, Description: draft.Description, Trigger: draft.Trigger,
		Steps: draft.Steps, Constraints: draft.Constraints,
		CreatedBy: "orchestrator", OriginRunID: st.run.ID, Confidence: draft.Confidence,
	})
	if err != nil {
		e.record(st, store.Step{Phase: "reflect",
			Outcome: "the proposal was rejected", Result: err.Error()})
		return
	}
	e.record(st, store.Step{Phase: "reflect", SkillID: created.ID,
		Outcome: "proposed a skill for review", Result: created.Name})
	e.emit("orch:draft", created)

	if e.mem != nil {
		_, _ = e.mem.Put(ctx, store.Memory{
			Kind: store.MemSystemLog, Scope: store.ScopeGlobal,
			ProjectID: st.req.ProjectID,
			Title:     "Run: " + truncate(st.req.Goal, 60),
			Body:      outcome, Source: "reflect",
		})
	}
}

// --- bookkeeping ------------------------------------------------------------

func (e *Engine) record(st *runState, step store.Step) {
	st.seq++
	step.RunID = st.run.ID
	step.Seq = st.seq
	saved, err := e.store.AddStep(step)
	if err != nil {
		return
	}
	st.steps = append(st.steps, saved)
	e.emit("orch:step", saved)
}

func (e *Engine) finish(st *runState, status store.RunStatus, summary, errText string) {
	_ = e.store.UpdateRunStatus(st.run.ID, status, summary, errText)
	st.run.Status = status
	st.run.Summary = summary
	st.run.Error = errText
	now := time.Now().Unix()
	st.run.EndedAt = &now
	e.emit("orch:run", st.run)
}

func (e *Engine) emitRun(st *runState) {
	if r, err := e.store.GetRun(st.run.ID); err == nil {
		e.emit("orch:run", r)
	}
}

func toolDefs(in []toolDefinition) []llm.ToolDef {
	out := make([]llm.ToolDef, 0, len(in))
	for _, d := range in {
		var t llm.ToolDef
		t.Type = "function"
		t.Function.Name = d.Name
		t.Function.Description = d.Description
		t.Function.Parameters = d.Schema
		out = append(out, t)
	}
	return out
}

func toPromptSkill(m skill.Match) promptSkill {
	ps := promptSkill{
		Name: m.Skill.Name, Trigger: m.Skill.Trigger,
		Score: m.Score, Constraints: m.Skill.Constraints,
	}
	for _, s := range m.Skill.Steps {
		ps.Steps = append(ps.Steps, promptStep{
			Order: s.Order, Description: s.Description,
			Tools: strings.Join(s.RecommendedTools, ", "),
		})
	}
	return ps
}

func memoryLine(m store.Memory) string {
	if m.Title == "" {
		return m.Body
	}
	return m.Title + ": " + m.Body
}

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return "(no output)"
	case string:
		return t
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// Cut from the middle: the head says what this is and the tail usually
	// holds the error, while the middle is the part nobody reads.
	half := max / 2
	return s[:half] + fmt.Sprintf("\n… [%d characters omitted] …\n", len(s)-max) + s[len(s)-half:]
}

func injectionNote(flagged bool, phrase string) string {
	if !flagged {
		return "done"
	}
	return fmt.Sprintf("contains instruction-shaped text: %q", truncate(phrase, 80))
}

func firstOrEmpty(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}
