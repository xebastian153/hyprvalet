package core

import "time"

// EventKind classifies one entry in the audit log: what became of one attempted
// capability call.
type EventKind string

const (
	EventRan          EventKind = "ran"           // executed successfully
	EventFailed       EventKind = "failed"        // permitted, but execution errored
	EventDenied       EventKind = "denied"        // policy refused it; nothing ran
	EventNeedsConfirm EventKind = "needs_confirm" // refused pending a human's approval
	EventDeclined     EventKind = "declined"      // a human was asked and said no
)

// Event is one immutable fact in the agent's history: an action that was
// attempted and what became of it. Events are append-only — they record what
// happened, they are never edited — which is what makes the log trustworthy as
// an audit trail and, later, usable for undo and replay. Refusals are recorded
// alongside executions on purpose: for a permission-gated agent, "it was asked
// for and denied" is as much a part of the story as "it ran".
type Event struct {
	At     time.Time // when the outcome was decided
	Source string    // which plane attempted it: "cli" or "daemon"
	Kind   EventKind // what became of it
	Cap    string    // capability ID
	Args   Args      // arguments of the call
	Detail string    // output, denial reason, or error text
}

// EventStore is the audit boundary of the hexagon: the core and its drivers
// append typed events and read them back; an adapter at the edge owns where and
// how they persist. Append failures must never block an action — auditing is an
// observer, not a gate — so callers log a warning and proceed.
type EventStore interface {
	// Append records one event at the end of the log.
	Append(Event) error
	// Tail returns up to n most recent events, oldest first.
	Tail(n int) ([]Event, error)
}
