package core

import "context"

// Args are the parameters passed to a capability. In M0 they arrive as strings
// from the CLI; the later LLM layer fills the exact same shape, so validation
// lives in one place regardless of caller.
type Args map[string]string

// Capability is a single typed action the agent can perform. Every capability
// declares what it touches (Access) and how risky it is (Risk), and validates
// its own arguments on every call. An invalid call returns a descriptive error
// instead of executing — so the caller, human or LLM, can correct and retry
// rather than the system doing the wrong thing.
type Capability interface {
	// ID is the stable, dotted identifier (e.g. "window.move_to_workspace").
	ID() string
	// Description is a one-line human- and LLM-readable summary.
	Description() string
	// Access reports what the action touches.
	Access() AccessKind
	// Risk reports the built-in risk tier.
	Risk() Risk
	// Params lists accepted parameter names, for help and validation.
	Params() []string
	// Run validates args and performs the action, returning a human-readable
	// result. It MUST NOT execute on invalid input.
	Run(ctx context.Context, args Args) (string, error)
}
