package core

import (
	"testing"
	"time"
)

func TestActionSignature(t *testing.T) {
	// Argument order must not change the signature.
	a := ActionSignature("app.open", Args{"cmd": "x", "flag": "y"})
	b := ActionSignature("app.open", Args{"flag": "y", "cmd": "x"})
	if a != b {
		t.Fatalf("signature is order-sensitive: %q vs %q", a, b)
	}
	// Different args must differ.
	if a == ActionSignature("app.open", Args{"cmd": "z", "flag": "y"}) {
		t.Fatal("different args produced the same signature")
	}
	// No args → bare id.
	if got := ActionSignature("workspace.list", nil); got != "workspace.list" {
		t.Fatalf("no-arg signature = %q, want bare id", got)
	}
}

func TestIsDoomLoop(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	window := 30 * time.Second
	sig := "workspace.switch workspace=2"

	hist := func(n int, age time.Duration) []ActionRecord {
		var h []ActionRecord
		for i := 0; i < n; i++ {
			h = append(h, ActionRecord{Signature: sig, At: now.Add(-age)})
		}
		return h
	}

	tests := []struct {
		name      string
		history   []ActionRecord
		threshold int
		want      bool
	}{
		{"first call, no history", nil, 3, false},
		{"second call (1 prior)", hist(1, time.Second), 3, false},
		{"third call (2 prior) trips at threshold 3", hist(2, time.Second), 3, true},
		{"priors outside the window do not count", hist(5, time.Minute), 3, false},
		{"threshold below 2 disables detection", hist(9, time.Second), 1, false},
		{"a different signature never counts", []ActionRecord{{Signature: "app.open cmd=x", At: now}}, 2, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsDoomLoop(tt.history, sig, now, window, tt.threshold); got != tt.want {
				t.Fatalf("IsDoomLoop = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPruneActions(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	window := 30 * time.Second
	history := []ActionRecord{
		{Signature: "keep", At: now.Add(-time.Second)},
		{Signature: "drop", At: now.Add(-time.Minute)},
	}
	got := PruneActions(history, now, window)
	if len(got) != 1 || got[0].Signature != "keep" {
		t.Fatalf("PruneActions = %+v, want only the recent record", got)
	}
}
