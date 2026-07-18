// Package protocol is the typed contract between the hyprvalet daemon and its
// clients — one shared package so both speak exactly the same shapes, never a
// hand-matched pair of encoders. Messages are JSON, one Request then one
// Response per exchange, streamed over a Unix socket.
package protocol

import "github.com/xebastian153/hyprvalet/internal/core"

// Op is the kind of a request.
type Op string

const (
	OpPing Op = "ping" // liveness + a capability count
	OpList Op = "list" // enumerate capabilities
	OpRun  Op = "run"  // run one capability by id with args
	// OpAsk and OpPlan carry a natural-language request in Text: the daemon
	// reasons (single intent / ordered plan), validates it against the allowlist,
	// binds each step to the current policy decision, and returns it WITHOUT
	// running anything. Execution is a separate OpRun per step, so the reasoning
	// (slow, stateless) and the mutation (fast, state-owning) stay cleanly split.
	OpAsk  Op = "ask"
	OpPlan Op = "plan"
	// OpEvaluate returns the current policy Decision for a capability without
	// running it — a dry-run the daemon uses to bind a plan against live arming
	// and session state it alone owns.
	OpEvaluate Op = "evaluate"
)

// Request is a typed command from a client to the daemon.
type Request struct {
	Op   Op                `json:"op"`
	Cap  string            `json:"cap,omitempty"`
	Args map[string]string `json:"args,omitempty"`
	// Approved is set on the follow-up a client sends after a needs_confirm
	// reply: the human said yes. It lets an Ask-tier action (or a doom-loop) run,
	// but never overrides a policy Deny — approval widens nothing the policy
	// forbids. The daemon re-evaluates on the approved call, so a world that
	// changed since the prompt still blocks.
	Approved bool `json:"approved,omitempty"`
	// Text is the natural-language request for OpAsk / OpPlan.
	Text string `json:"text,omitempty"`
	// Escalate asks the daemon to reason with its stronger model. A client sets
	// it on the final corrective attempt, after the default model failed to fix
	// its own mistake. Escalation changes only reasoning depth — the resulting
	// intent walks exactly the same allowlist, policy gate, and validation.
	Escalate bool `json:"escalate,omitempty"`
}

// Status is the outcome class of a response, so a client can branch without
// parsing prose.
type Status string

const (
	StatusPong         Status = "pong"
	StatusCaps         Status = "caps"
	StatusRan          Status = "ran"
	StatusDenied       Status = "denied"        // policy denied the action
	StatusNeedsConfirm Status = "needs_confirm" // would run only with a human's approval
	StatusError        Status = "error"         // malformed request, unknown capability, run failure
	StatusPlanned      Status = "planned"       // a reasoned, policy-bound plan (ask/plan), nothing run yet
	StatusDecision     Status = "decision"      // a dry-run policy decision (evaluate)
)

// Response is the daemon's reply to one Request.
type Response struct {
	Status    Status     `json:"status"`
	Text      string     `json:"text,omitempty"`      // human-readable result or reason
	Error     string     `json:"error,omitempty"`     // set when Status is error
	Caps      []CapInfo  `json:"caps,omitempty"`      // set when Status is caps
	Count     int        `json:"count,omitempty"`     // capability count, for pong
	Summary   string     `json:"summary,omitempty"`   // the plan's one-line description, for planned
	Reasoning string     `json:"reasoning,omitempty"` // the model's rationale for a single intent (ask)
	Plan      []PlanStep `json:"plan,omitempty"`      // the reasoned, policy-bound steps, for planned
	// Retryable marks an error as a capability argument rejection — the model's
	// mistake, correctable by re-asking it with this error as feedback. Runtime
	// failures (a dead tool, a broken pipe) are not retryable: re-asking the
	// model cannot fix the world.
	Retryable bool `json:"retryable,omitempty"`
}

// PlanStep is the wire view of one reasoned step: a chosen capability, the args
// the model filled, and the policy decision the daemon bound to it against live
// state. A client previews these, refuses the plan if any step is a "deny", and
// otherwise runs them one OpRun at a time.
type PlanStep struct {
	Cap      string            `json:"cap"`
	Args     map[string]string `json:"args,omitempty"`
	Decision string            `json:"decision"` // "allow" | "ask" | "deny"
}

// CapInfo is the wire view of a capability — the core.Capability interface
// flattened to serializable fields.
type CapInfo struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Access      string   `json:"access"`
	Risk        string   `json:"risk"`
	Params      []string `json:"params"`
}

// CapInfoOf flattens a core.Capability for the wire.
func CapInfoOf(c core.Capability) CapInfo {
	return CapInfo{
		ID:          c.ID(),
		Description: c.Description(),
		Access:      string(c.Access()),
		Risk:        c.Risk().String(),
		Params:      c.Params(),
	}
}
