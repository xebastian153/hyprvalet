package core

import (
	"testing"
	"time"
)

func TestArmStateIsArmed(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	state := ArmState{
		"live":    now.Add(time.Minute),
		"expired": now.Add(-time.Minute),
	}
	tests := []struct {
		name string
		id   string
		want bool
	}{
		{"armed and unexpired", "live", true},
		{"armed but expired", "expired", false},
		{"never armed", "absent", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := state.IsArmed(tt.id, now); got != tt.want {
				t.Fatalf("IsArmed(%q) = %v, want %v", tt.id, got, tt.want)
			}
		})
	}
}

func TestArmStateArmExpiresAtBoundary(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	state := ArmState{}
	state.Arm("cap", now, time.Minute)

	if !state.IsArmed("cap", now.Add(59*time.Second)) {
		t.Fatal("should be armed one second before expiry")
	}
	// The window is [now, now+dur): the expiry instant itself is not armed.
	if state.IsArmed("cap", now.Add(time.Minute)) {
		t.Fatal("should not be armed at the exact expiry instant")
	}
}

func TestArmStateDisarm(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	state := ArmState{"cap": now.Add(time.Hour)}
	state.Disarm("cap")
	if state.IsArmed("cap", now) {
		t.Fatal("Disarm should revoke immediately")
	}
	state.Disarm("absent") // no-op, must not panic
}

func TestArmStatePrune(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	state := ArmState{
		"keep":       now.Add(time.Minute),
		"drop":       now.Add(-time.Minute),
		"drop-exact": now,
	}
	state.Prune(now)

	if _, ok := state["keep"]; !ok {
		t.Error("Prune dropped a live grant")
	}
	if _, ok := state["drop"]; ok {
		t.Error("Prune kept an expired grant")
	}
	if _, ok := state["drop-exact"]; ok {
		t.Error("Prune kept a grant expiring exactly at now")
	}
}
