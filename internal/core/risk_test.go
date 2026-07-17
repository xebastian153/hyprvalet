package core

import "testing"

func TestRiskString(t *testing.T) {
	tests := []struct {
		name string
		risk Risk
		want string
	}{
		{"safe tier", RiskSafe, "safe"},
		{"confirm tier", RiskConfirm, "confirm"},
		{"forbidden tier", RiskForbidden, "forbidden"},
		{"out-of-range tier falls back to unknown", Risk(99), "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.risk.String(); got != tt.want {
				t.Fatalf("Risk(%d).String() = %q, want %q", tt.risk, got, tt.want)
			}
		})
	}
}
