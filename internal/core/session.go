package core

import "time"

// SessionAllow is the set of capabilities the user has approved for the rest of
// the login session by answering "always" at a prompt. It upgrades an Ask
// decision to Allow so the same action stops prompting — but it never overrides
// a Deny, so it can neither bypass arming nor widen what the policy forbids. It
// is persisted under the runtime dir and cleared on logout, so a session grant
// never outlives the session that made it.
type SessionAllow map[string]bool

// Has reports whether id carries a session-wide allow grant.
func (s SessionAllow) Has(id string) bool { return s[id] }

// Allow records a session-wide grant for id.
func (s SessionAllow) Allow(id string) { s[id] = true }

// Decide is Evaluate plus session grants: a capability the user chose to
// "always" allow this session resolves to Allow without re-prompting. Grants
// only upgrade Ask to Allow — a Deny (from policy or unmet arming) stays Deny —
// so a grant can never widen what is forbidden.
func Decide(rules PolicyRules, arm ArmState, session SessionAllow, c Capability, now time.Time) Decision {
	d := Evaluate(rules, arm, c, now)
	if d == DecisionAsk && session.Has(c.ID()) {
		return DecisionAllow
	}
	return d
}
