package core

import "time"

// Decision is the outcome of the permission gate for a single capability call.
// It is the "if" — deliberately separate from AccessKind (the "what") and from
// the mechanism that carries the action out. A policy turns a capability into a
// Decision; the caller (CLI today, daemon later) enforces it.
//
// The zero value is DecisionAsk on purpose: anything unconfigured fails toward
// asking a human, never toward silently allowing.
type Decision int

const (
	DecisionAsk   Decision = iota // require explicit human approval (safe default)
	DecisionAllow                 // run without prompting
	DecisionDeny                  // refuse; never run
)

func (d Decision) String() string {
	switch d {
	case DecisionAsk:
		return "ask"
	case DecisionAllow:
		return "allow"
	case DecisionDeny:
		return "deny"
	default:
		return "unknown"
	}
}

// Rule is the installer-owned policy for one capability or one class of them.
// A rule is applied as a whole — the most specific matching rule wins; fields
// are never merged across levels, so a rule always fully describes its own
// intent and there are no surprising inherited flags.
type Rule struct {
	// Decision is what to do when the capability is allowed to run at all.
	Decision Decision
	// RequiresArming gates the capability behind a temporal grant: until it is
	// armed, Evaluate returns DecisionDeny regardless of Decision. Once armed
	// (and unexpired), Decision applies. This is "off by default, on for a
	// bounded window" rather than an indefinite "remember for this session".
	RequiresArming bool
	// ArmFor is how long an arm grant for this capability lasts. Zero means fall
	// back to PolicyRules.DefaultArmFor. Evaluate does not read this; the `arm`
	// command does, when it computes an expiry.
	ArmFor time.Duration
}

// PolicyRules is the resolved, installer-owned policy. It is plain domain data;
// an adapter at the edge loads it from the user's config file so the core never
// depends on a file format. Precedence, most specific first:
//
//	per-capability ID  >  per-AccessKind  >  per-Risk tier  >  Default
type PolicyRules struct {
	Default       Rule
	ByRisk        map[Risk]Rule
	ByAccess      map[AccessKind]Rule
	ByCapID       map[string]Rule
	DefaultArmFor time.Duration
}

// Resolve returns the effective Rule for a capability, applying precedence. The
// most specific rule that exists wins as a whole.
func (p PolicyRules) Resolve(c Capability) Rule {
	if r, ok := p.ByCapID[c.ID()]; ok {
		return r
	}
	if r, ok := p.ByAccess[c.Access()]; ok {
		return r
	}
	if r, ok := p.ByRisk[c.Risk()]; ok {
		return r
	}
	return p.Default
}

// ArmFor reports how long an arm grant should last for a capability: its own
// ArmFor if set, otherwise the policy-wide default.
func (p PolicyRules) ArmFor(c Capability) time.Duration {
	if r, ok := p.ByCapID[c.ID()]; ok && r.ArmFor > 0 {
		return r.ArmFor
	}
	return p.DefaultArmFor
}

// Evaluate decides what to do with a capability call at instant now, given the
// installer's rules and the current arming state. It is a pure function: same
// inputs, same Decision, no I/O — so the whole permission model is testable
// without a config file, a clock, or a live desktop.
func Evaluate(rules PolicyRules, arm ArmState, c Capability, now time.Time) Decision {
	r := rules.Resolve(c)
	if r.RequiresArming && !arm.IsArmed(c.ID(), now) {
		return DecisionDeny
	}
	return r.Decision
}
