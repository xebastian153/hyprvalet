package policyfile

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
)

func TestActionLogRoundTrip(t *testing.T) {
	now := time.Date(2026, 7, 17, 20, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "action-log.json")

	history := []core.ActionRecord{
		{Signature: "workspace.switch workspace=2", At: now},
		{Signature: "app.open cmd=firefox", At: now.Add(-time.Second)},
	}
	if err := SaveActionLog(path, history); err != nil {
		t.Fatalf("SaveActionLog: %v", err)
	}

	loaded, err := LoadActionLog(path)
	if err != nil {
		t.Fatalf("LoadActionLog: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("round-tripped %d records, want 2", len(loaded))
	}
	if loaded[0].Signature != "workspace.switch workspace=2" || !loaded[0].At.Equal(now) {
		t.Fatalf("record 0 wrong: %+v", loaded[0])
	}
}

func TestLoadActionLogMissingFileIsEmpty(t *testing.T) {
	got, err := LoadActionLog(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing file should be empty, got %d", len(got))
	}
}
