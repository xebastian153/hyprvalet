package prompt

import (
	"strings"
	"testing"
)

func TestParseWatchDesign(t *testing.T) {
	turn := ParseWatch(`{"state":"design","question":"¿email o usuario para login?","answer":"Usá email."}`)
	if turn.State != WatchDesign || turn.Answer != "Usá email." {
		t.Fatalf("design parse wrong: %+v", turn)
	}
}

func TestParseWatchPermission(t *testing.T) {
	turn := ParseWatch(`{"state":"permission","question":"run rm -rf build/","answer":""}`)
	if turn.State != WatchPermission {
		t.Fatalf("permission parse wrong: %+v", turn)
	}
}

func TestParseWatchDesignWithoutAnswerFailsSafe(t *testing.T) {
	// A design turn with nothing to say must NOT relay anything.
	turn := ParseWatch(`{"state":"design","question":"something?","answer":""}`)
	if turn.State != WatchWorking {
		t.Fatalf("empty-answer design must downgrade to working, got %+v", turn)
	}
}

func TestParseWatchUnknownStateFailsSafe(t *testing.T) {
	turn := ParseWatch(`{"state":"approve_everything","answer":"yes"}`)
	if turn.State != WatchWorking {
		t.Fatalf("unknown state must fail safe to working, got %+v", turn)
	}
}

func TestParseWatchGarbageFailsSafe(t *testing.T) {
	if turn := ParseWatch("not json at all"); turn.State != WatchWorking {
		t.Fatalf("garbage must fail safe to working, got %+v", turn)
	}
}

func TestBuildWatchCarriesConservativeRule(t *testing.T) {
	p := BuildWatch("Project plan — Shop")
	if !strings.Contains(p, "when in doubt") && !strings.Contains(strings.ToLower(p), "when in doubt") {
		t.Fatalf("watch prompt must carry the when-in-doubt-is-permission rule")
	}
	if !strings.Contains(p, "NEVER answer") {
		t.Fatalf("watch prompt must forbid answering permission prompts")
	}
}
