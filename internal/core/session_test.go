package core

import (
	"testing"
	"time"
)

func TestDecideSessionGrant(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	rules := PolicyRules{
		ByCapID: map[string]Rule{
			"ask.cap":   {Decision: DecisionAsk},
			"armed.cap": {Decision: DecisionAsk, RequiresArming: true},
			"deny.cap":  {Decision: DecisionDeny},
			"allow.cap": {Decision: DecisionAllow},
		},
	}
	askCap := polCap{"ask.cap", AccessCommand, RiskConfirm}
	armedCap := polCap{"armed.cap", AccessApp, RiskConfirm}
	denyCap := polCap{"deny.cap", AccessCommand, RiskConfirm}
	allowCap := polCap{"allow.cap", AccessWorkspace, RiskSafe}

	t.Run("grant upgrades Ask to Allow", func(t *testing.T) {
		session := SessionAllow{"ask.cap": true}
		if got := Decide(rules, ArmState{}, session, askCap, now); got != DecisionAllow {
			t.Fatalf("granted Ask = %v, want Allow", got)
		}
	})

	t.Run("no grant leaves Ask as Ask", func(t *testing.T) {
		if got := Decide(rules, ArmState{}, SessionAllow{}, askCap, now); got != DecisionAsk {
			t.Fatalf("ungranted Ask = %v, want Ask", got)
		}
	})

	t.Run("grant never overrides a policy Deny", func(t *testing.T) {
		session := SessionAllow{"deny.cap": true}
		if got := Decide(rules, ArmState{}, session, denyCap, now); got != DecisionDeny {
			t.Fatalf("granted Deny = %v, want Deny (a grant cannot widen policy)", got)
		}
	})

	t.Run("grant never bypasses unmet arming", func(t *testing.T) {
		// armed.cap requires arming and is not armed → Evaluate is Deny, which a
		// session grant must not upgrade.
		session := SessionAllow{"armed.cap": true}
		if got := Decide(rules, ArmState{}, session, armedCap, now); got != DecisionDeny {
			t.Fatalf("granted unarmed = %v, want Deny (a grant cannot bypass arming)", got)
		}
	})

	t.Run("allow stays allow regardless of grants", func(t *testing.T) {
		if got := Decide(rules, ArmState{}, SessionAllow{}, allowCap, now); got != DecisionAllow {
			t.Fatalf("Allow = %v, want Allow", got)
		}
	})

	t.Run("Allow records a grant that Decide then honors", func(t *testing.T) {
		session := SessionAllow{}
		session.Allow("ask.cap")
		if !session.Has("ask.cap") {
			t.Fatal("Allow did not record the grant")
		}
		if got := Decide(rules, ArmState{}, session, askCap, now); got != DecisionAllow {
			t.Fatalf("after Allow, Decide = %v, want Allow", got)
		}
	})
}
