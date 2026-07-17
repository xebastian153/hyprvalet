package policyfile

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/SebasDevMag/hyprvalet/internal/core"
)

func TestArmStateRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "armed.json")

	state := core.ArmState{"app.open": now.Add(time.Hour)}
	if err := SaveArmState(path, state, now); err != nil {
		t.Fatalf("SaveArmState: %v", err)
	}

	loaded, err := LoadArmState(path, now)
	if err != nil {
		t.Fatalf("LoadArmState: %v", err)
	}
	if !loaded.IsArmed("app.open", now) {
		t.Fatal("round-tripped grant is not armed")
	}
}

func TestSaveArmStatePrunesExpired(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "armed.json")

	state := core.ArmState{
		"live": now.Add(time.Hour),
		"dead": now.Add(-time.Hour),
	}
	if err := SaveArmState(path, state, now); err != nil {
		t.Fatalf("SaveArmState: %v", err)
	}

	loaded, err := LoadArmState(path, now)
	if err != nil {
		t.Fatalf("LoadArmState: %v", err)
	}
	if _, ok := loaded["dead"]; ok {
		t.Error("expired grant was persisted")
	}
	if _, ok := loaded["live"]; !ok {
		t.Error("live grant was dropped")
	}
}

func TestLoadArmStateMissingFileIsEmpty(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	state, err := LoadArmState(filepath.Join(t.TempDir(), "absent.json"), now)
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(state) != 0 {
		t.Fatalf("missing file should yield empty state, got %d entries", len(state))
	}
}
