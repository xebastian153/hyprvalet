package memory

import (
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

var (
	_ core.Capability = remember{}
	_ core.Capability = recall{}
	_ core.Capability = forget{}
)

// isolate points the store at a temp file for the duration of a test.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

func TestRememberThenRecall(t *testing.T) {
	isolate(t)
	if err := Remember("the user prefers Postgres for databases"); err != nil {
		t.Fatalf("remember: %v", err)
	}
	if err := Remember("the user's name is Sebas"); err != nil {
		t.Fatalf("remember: %v", err)
	}
	hits := Search("what database should we use", 8)
	if len(hits) != 1 || hits[0].Text != "the user prefers Postgres for databases" {
		t.Fatalf("expected the Postgres note, got %v", hits)
	}
}

func TestRecentIsBounded(t *testing.T) {
	isolate(t)
	for i := 0; i < 20; i++ {
		Remember("note " + string(rune('a'+i)))
	}
	if got := Recent(5); len(got) != 5 {
		t.Fatalf("Recent(5) returned %d notes", len(got))
	}
	// Most recent kept: the last note must be present, oldest dropped.
	all := Recent(5)
	if all[len(all)-1].Text != "note "+string(rune('a'+19)) {
		t.Fatalf("Recent should keep the newest note, got %q", all[len(all)-1].Text)
	}
}

func TestForgetRemovesMatches(t *testing.T) {
	isolate(t)
	Remember("the deploy key lives in vault")
	Remember("the user likes dark themes")
	out, err := (forget{}).Run(nil, core.Args{"query": "deploy key"})
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if out != "forgot 1 note(s)" {
		t.Fatalf("unexpected forget result: %q", out)
	}
	if len(All()) != 1 || All()[0].Text != "the user likes dark themes" {
		t.Fatalf("forget removed the wrong note: %v", All())
	}
}

func TestRememberRejectsEmpty(t *testing.T) {
	isolate(t)
	if _, err := (remember{}).Run(nil, core.Args{"text": "  "}); !core.IsValidation(err) {
		t.Fatalf("empty remember must be a ValidationError, got %v", err)
	}
}

func TestForgetIsConfirmTier(t *testing.T) {
	// Erasing memory is destructive — it must ask first.
	if (forget{}).Risk() != core.RiskConfirm {
		t.Fatal("memory.forget must be Confirm-tier")
	}
}

func TestMissingStoreIsEmptyNotError(t *testing.T) {
	isolate(t)
	if got := All(); got != nil {
		t.Fatalf("a fresh memory should be empty, got %v", got)
	}
	if hits := Search("anything", 8); hits != nil {
		t.Fatalf("search on empty memory should find nothing, got %v", hits)
	}
}
