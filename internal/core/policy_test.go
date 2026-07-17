package core

import (
	"context"
	"testing"
	"time"
)

// polCap is a configurable Capability for policy tests: its Access and Risk can
// be set per case so precedence can be exercised. (registry_test.go's fakeCap is
// fixed, hence a second fake here.)
type polCap struct {
	id     string
	access AccessKind
	risk   Risk
}

func (c polCap) ID() string                              { return c.id }
func (polCap) Description() string                       { return "pol" }
func (c polCap) Access() AccessKind                      { return c.access }
func (c polCap) Risk() Risk                              { return c.risk }
func (polCap) Params() []string                          { return nil }
func (polCap) Run(context.Context, Args) (string, error) { return "", nil }

func TestDecisionString(t *testing.T) {
	tests := []struct {
		d    Decision
		want string
	}{
		{DecisionAsk, "ask"},
		{DecisionAllow, "allow"},
		{DecisionDeny, "deny"},
		{Decision(42), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.d.String(); got != tt.want {
			t.Errorf("Decision(%d).String() = %q, want %q", tt.d, got, tt.want)
		}
	}
}

func TestDecisionZeroValueIsAsk(t *testing.T) {
	// Fail-safe: an unconfigured decision must ask, never allow.
	var d Decision
	if d != DecisionAsk {
		t.Fatalf("zero Decision = %v, want DecisionAsk (fail toward asking)", d)
	}
}

func TestPolicyResolvePrecedence(t *testing.T) {
	rules := PolicyRules{
		Default:  Rule{Decision: DecisionAsk},
		ByRisk:   map[Risk]Rule{RiskSafe: {Decision: DecisionAllow}},
		ByAccess: map[AccessKind]Rule{AccessCommand: {Decision: DecisionDeny}},
		ByCapID:  map[string]Rule{"exact.match": {Decision: DecisionAllow}},
	}
	tests := []struct {
		name string
		cap  polCap
		want Decision
	}{
		{"capability ID wins over everything", polCap{"exact.match", AccessCommand, RiskSafe}, DecisionAllow},
		{"AccessKind wins over Risk", polCap{"other", AccessCommand, RiskSafe}, DecisionDeny},
		{"Risk wins over Default", polCap{"other", AccessWindow, RiskSafe}, DecisionAllow},
		{"Default when nothing matches", polCap{"other", AccessWindow, RiskConfirm}, DecisionAsk},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rules.Resolve(tt.cap).Decision; got != tt.want {
				t.Fatalf("Resolve(%+v).Decision = %v, want %v", tt.cap, got, tt.want)
			}
		})
	}
}

func TestEvaluateArming(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	rules := PolicyRules{
		ByCapID: map[string]Rule{
			"app.open": {Decision: DecisionAsk, RequiresArming: true},
		},
	}
	cap := polCap{"app.open", AccessApp, RiskConfirm}

	t.Run("denied when armed capability is not armed", func(t *testing.T) {
		if got := Evaluate(rules, ArmState{}, cap, now); got != DecisionDeny {
			t.Fatalf("Evaluate unarmed = %v, want DecisionDeny", got)
		}
	})

	t.Run("falls through to its Decision once armed", func(t *testing.T) {
		arm := ArmState{"app.open": now.Add(time.Minute)}
		if got := Evaluate(rules, arm, cap, now); got != DecisionAsk {
			t.Fatalf("Evaluate armed = %v, want DecisionAsk", got)
		}
	})

	t.Run("denied again after the grant expires", func(t *testing.T) {
		arm := ArmState{"app.open": now.Add(-time.Second)}
		if got := Evaluate(rules, arm, cap, now); got != DecisionDeny {
			t.Fatalf("Evaluate expired = %v, want DecisionDeny", got)
		}
	})
}

func TestPolicyArmForFallback(t *testing.T) {
	rules := PolicyRules{
		DefaultArmFor: 5 * time.Minute,
		ByCapID: map[string]Rule{
			"long.arm": {RequiresArming: true, ArmFor: 30 * time.Minute},
		},
	}
	t.Run("uses the capability's own ArmFor when set", func(t *testing.T) {
		got := rules.ArmFor(polCap{"long.arm", AccessCommand, RiskConfirm})
		if got != 30*time.Minute {
			t.Fatalf("ArmFor = %v, want 30m", got)
		}
	})
	t.Run("falls back to DefaultArmFor otherwise", func(t *testing.T) {
		got := rules.ArmFor(polCap{"unset", AccessCommand, RiskConfirm})
		if got != 5*time.Minute {
			t.Fatalf("ArmFor = %v, want 5m", got)
		}
	})
}
