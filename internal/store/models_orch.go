package store

// RunStatus is where an orchestrator run got to.
type RunStatus string

const (
	RunRunning   RunStatus = "running"
	RunWaiting   RunStatus = "waiting_approval"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// Run is one pass of the orchestrator loop.
type Run struct {
	ID string `json:"id"`
	// Goal is what it was asked to achieve, in the words it was asked in.
	Goal string `json:"goal"`
	// Trigger records who started it. A scheduled run has a smaller tool set
	// than a run a person asked for, so knowing which it was is not trivia.
	Trigger   string    `json:"trigger"`
	ProjectID string    `json:"projectId"`
	Status    RunStatus `json:"status"`
	Model     string    `json:"model"`
	SkillIDs  []string  `json:"skillIds"`
	StartedAt int64     `json:"startedAt"`
	EndedAt   *int64    `json:"endedAt"`
	Summary   string    `json:"summary"`
	Error     string    `json:"error"`
}

// Step is one thing that happened during a run.
//
// Every step is written down, including the ones that were refused. A run where
// the interesting event was "it wanted to do X and was not allowed" reads as an
// empty run otherwise.
type Step struct {
	ID     string `json:"id"`
	RunID  string `json:"runId"`
	Seq    int    `json:"seq"`
	Phase  string `json:"phase"` // observe | retrieve | plan | act | reflect
	Tool   string `json:"tool"`
	Args   string `json:"args"`
	Result string `json:"result"`
	// Reasoning is what the model said it was doing and why, in its own words.
	Reasoning string   `json:"reasoning"`
	SkillID   string   `json:"skillId"`
	MemoryIDs []string `json:"memoryIds"`
	// InjectionFlag marks a step whose input contained text shaped like an
	// instruction. It never blocks anything — the false positive rate is far
	// too high for that — but it has to be visible.
	InjectionFlag bool   `json:"injectionFlag"`
	Risk          string `json:"risk"`
	// Outcome is what became of a proposed call: done, denied, rejected,
	// expired, failed.
	Outcome    string `json:"outcome"`
	DurationMs int64  `json:"durationMs"`
	CreatedAt  int64  `json:"createdAt"`
}

// ApprovalStatus is the state of one request for permission.
type ApprovalStatus string

const (
	ApprovalPending  ApprovalStatus = "pending"
	ApprovalApproved ApprovalStatus = "approved"
	ApprovalRejected ApprovalStatus = "rejected"
	ApprovalExpired  ApprovalStatus = "expired"
)

// Approval is a tool call waiting for a person.
type Approval struct {
	ID    string `json:"id"`
	RunID string `json:"runId"`
	Tool  string `json:"tool"`
	Args  string `json:"args"`
	Risk  string `json:"risk"`
	// Rationale is why the model says this is needed now. It is the one thing
	// a person reads before deciding, so it is stored verbatim.
	Rationale     string         `json:"rationale"`
	Target        string         `json:"target"`
	SkillID       string         `json:"skillId"`
	InjectionFlag bool           `json:"injectionFlag"`
	Status        ApprovalStatus `json:"status"`
	DecidedAt     *int64         `json:"decidedAt"`
	Note          string         `json:"note"`
	CreatedAt     int64          `json:"createdAt"`
	ExpiresAt     int64          `json:"expiresAt"`
}
