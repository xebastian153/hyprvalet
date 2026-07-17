package core

import "time"

// ArmState is the set of capabilities currently armed, each mapped to the
// instant its grant expires. Arming makes a dangerous capability available for a
// bounded window instead of an indefinite "remember for this session" — every
// grant auto-expires. The pattern comes from openclaw's temporal arming of
// dangerous capability groups.
//
// It is plain domain data: an adapter persists it (a one-shot CLI needs the
// grant to survive across invocations) and injects "now". The logic here never
// touches a clock or the filesystem, so it stays pure and testable.
type ArmState map[string]time.Time

// IsArmed reports whether the capability id is armed and not yet expired at now.
func (a ArmState) IsArmed(id string, now time.Time) bool {
	until, ok := a[id]
	return ok && now.Before(until)
}

// Arm grants id a window lasting dur from now, replacing any existing grant.
func (a ArmState) Arm(id string, now time.Time, dur time.Duration) {
	a[id] = now.Add(dur)
}

// Disarm revokes id immediately. Revoking an unarmed capability is a no-op.
func (a ArmState) Disarm(id string) {
	delete(a, id)
}

// Prune drops every grant that has expired at now. An adapter calls it before
// persisting so the stored state never accumulates stale entries.
func (a ArmState) Prune(now time.Time) {
	for id, until := range a {
		if !now.Before(until) {
			delete(a, id)
		}
	}
}
