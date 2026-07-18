package policyfile

import (
	"path/filepath"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

func TestSessionAllowRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session-allow.json")

	s := core.SessionAllow{"omarchy.run": true, "app.open": true}
	if err := SaveSessionAllow(path, s); err != nil {
		t.Fatalf("SaveSessionAllow: %v", err)
	}

	loaded, err := LoadSessionAllow(path)
	if err != nil {
		t.Fatalf("LoadSessionAllow: %v", err)
	}
	if !loaded.Has("omarchy.run") || !loaded.Has("app.open") {
		t.Fatalf("round-tripped grants missing: %+v", loaded)
	}
	if loaded.Has("workspace.switch") {
		t.Error("loaded a grant that was never saved")
	}
}

func TestLoadSessionAllowMissingFileIsEmpty(t *testing.T) {
	s, err := LoadSessionAllow(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(s) != 0 {
		t.Fatalf("missing file should be empty, got %d grants", len(s))
	}
}
