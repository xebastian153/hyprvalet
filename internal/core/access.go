package core

// AccessKind describes WHAT a capability touches. It is deliberately kept
// separate from the Decision of whether an action is allowed (the "if"). This
// what-vs-if split is the backbone of the permission model: intent maps to an
// AccessKind, and a separate policy layer turns that into allow / ask / deny.
type AccessKind string

const (
	AccessApp       AccessKind = "app"
	AccessWindow    AccessKind = "window"
	AccessWorkspace AccessKind = "workspace"
	AccessCommand   AccessKind = "command"
)

// Risk is the built-in risk tier of a capability. It feeds the permission gate:
// Safe actions may run automatically, Confirm actions require human approval,
// Forbidden actions never run.
type Risk int

const (
	RiskSafe      Risk = iota // reversible / low-impact — may auto-run
	RiskConfirm               // destructive or high-impact — ask first
	RiskForbidden             // never runs
)

func (r Risk) String() string {
	switch r {
	case RiskSafe:
		return "safe"
	case RiskConfirm:
		return "confirm"
	case RiskForbidden:
		return "forbidden"
	default:
		return "unknown"
	}
}
