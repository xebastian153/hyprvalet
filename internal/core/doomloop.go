package core

import (
	"sort"
	"strings"
	"time"
)

// ActionRecord is one past capability execution: a canonical signature and when
// it ran. A short rolling history of these lets the gate notice a degenerate
// loop — the same action firing over and over with real side effects — and force
// a confirmation before it does more damage.
type ActionRecord struct {
	Signature string
	At        time.Time
}

// ActionSignature canonicalizes a capability call so repeats compare equal: the
// id followed by its arguments in sorted key=value order.
func ActionSignature(id string, args Args) string {
	if len(args) == 0 {
		return id
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return id + " " + strings.Join(parts, " ")
}

// IsDoomLoop reports whether running sig now would be at least the threshold-th
// identical call within window — the signal to force a confirmation before a
// degenerate loop keeps going. It counts the prospective call plus prior records
// with the same signature still inside the window. A threshold below 2 disables
// detection.
func IsDoomLoop(history []ActionRecord, sig string, now time.Time, window time.Duration, threshold int) bool {
	if threshold < 2 {
		return false
	}
	count := 1 // the prospective call
	cutoff := now.Add(-window)
	for _, r := range history {
		if r.Signature == sig && r.At.After(cutoff) {
			count++
		}
	}
	return count >= threshold
}

// PruneActions drops records older than window ending at now, keeping the log
// bounded to what doom-loop detection needs. It returns a fresh slice.
func PruneActions(history []ActionRecord, now time.Time, window time.Duration) []ActionRecord {
	cutoff := now.Add(-window)
	out := make([]ActionRecord, 0, len(history))
	for _, r := range history {
		if r.At.After(cutoff) {
			out = append(out, r)
		}
	}
	return out
}
