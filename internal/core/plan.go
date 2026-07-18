package core

import (
	"context"
	"fmt"
)

// Plan is a model-generated, ordered sequence of capability calls that fulfills
// a natural-language request. It is the multi-step counterpart to Intent: where
// Intent is a single action, a Plan is several, previewed and confirmed as a
// whole before any of it runs. Structurally it mirrors a Recipe — same Step,
// same lifecycle guard — but a Plan is generated on the fly, not authored.
type Plan struct {
	Request string // the original natural-language request
	Summary string // the model's one-line description of what the plan does
	Steps   []Step // ordered capability calls; Step is shared with recipes
	// Reply is the conversational answer when the request is talk, not action
	// (Steps empty). Like Intent.Reply it is words, not execution — it never
	// reaches the permission gate.
	Reply string
}

// PlannerPort is the multi-step reasoning boundary of the hexagon. An adapter
// (e.g. a local Ollama model) implements it; the core depends only on this
// interface. Plan maps a natural-language request to an ordered Plan, choosing
// only from the capabilities it is given, so the caller controls the menu.
// recent is the agent's episodic memory (see LLMPort); nil means no memory.
//
// A returned error means the reasoning itself failed (model unreachable,
// timeout, unparseable output). A request the model cannot fulfill with the
// available capabilities returns a Plan with no steps, not an error.
type PlannerPort interface {
	Plan(ctx context.Context, request string, caps []Capability, recent []Event) (Plan, error)
}

// Validate checks a plan is safe and runnable against the registry: it has at
// least one step, every step names a registered capability (the allowlist check
// that makes a hallucinated step harmless), and it passes the lifecycle guard —
// so a generated plan, like a recipe, can never restart or kill the host. It
// does not validate step arguments; each capability does that on its own Run.
func (p Plan) Validate(reg *Registry) error {
	if len(p.Steps) == 0 {
		return fmt.Errorf("plan has no steps")
	}
	for i, s := range p.Steps {
		if _, ok := reg.Get(s.Capability); !ok {
			return fmt.Errorf("plan step %d: model chose %q, not a registered capability", i+1, s.Capability)
		}
	}
	return checkLifecycle(p.Steps, "plan")
}
