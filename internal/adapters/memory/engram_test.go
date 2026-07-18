package memory

import "testing"

func TestParseSearch(t *testing.T) {
	out := `Found 2 memories:

[1] #7 (preference) — Preferencia de BD
    El usuario prefiere Postgres para bases de datos
    2026-07-18 16:17:31 | project: jarvis | scope: project

[2] #3 (note) — Nombre
    me llamo Sebas
    2026-07-18 16:10:00 | project: jarvis | scope: project
`
	got := parseSearch(out)
	if len(got) != 2 {
		t.Fatalf("expected 2 notes, got %d: %v", len(got), got)
	}
	if got[0].Text != "El usuario prefiere Postgres para bases de datos" {
		t.Fatalf("first note wrong: %q", got[0].Text)
	}
	if got[1].Text != "me llamo Sebas" {
		t.Fatalf("second note wrong: %q", got[1].Text)
	}
}

func TestParseSearchEmpty(t *testing.T) {
	if got := parseSearch(`No memories found for: "xyz"`); got != nil {
		t.Fatalf("expected no notes, got %v", got)
	}
}

func TestParseSearchFallsBackToTitle(t *testing.T) {
	// A hit with only a header (no message line) still yields the title.
	out := "Found 1 memories:\n\n[1] #9 (note) — just a title\n"
	got := parseSearch(out)
	if len(got) != 1 || got[0].Text != "just a title" {
		t.Fatalf("title fallback failed: %v", got)
	}
}

func TestParseContext(t *testing.T) {
	out := `## Memory from Previous Sessions

### Recent Sessions
- **jarvis** (2026-07-18 16:17:31) [2 observations]

### Recent Observations
- [note] **Test JSON**: probando salida json
- [preference] **Preferencia de BD**: El usuario prefiere Postgres para bases de datos
`
	got := parseContext(out, 10)
	if len(got) != 2 {
		t.Fatalf("expected 2 observations, got %d: %v", len(got), got)
	}
	if got[0].Text != "probando salida json" {
		t.Fatalf("first obs wrong: %q", got[0].Text)
	}
	if got[1].Text != "El usuario prefiere Postgres para bases de datos" {
		t.Fatalf("second obs wrong: %q", got[1].Text)
	}
}

func TestParseContextRespectsLimit(t *testing.T) {
	out := "### Recent Observations\n- [note] **A**: one\n- [note] **B**: two\n- [note] **C**: three\n"
	if got := parseContext(out, 2); len(got) != 2 {
		t.Fatalf("limit ignored: got %d", len(got))
	}
}

func TestParseContextIgnoresOtherSections(t *testing.T) {
	// Sessions and prompts must not be mistaken for observations.
	out := "### Recent Sessions\n- [note] **X**: not an observation\n### Recent Observations\n- [note] **Y**: yes\n"
	got := parseContext(out, 10)
	if len(got) != 1 || got[0].Text != "yes" {
		t.Fatalf("section scoping failed: %v", got)
	}
}

func TestParseIDs(t *testing.T) {
	out := "[1] #7 (note) — a\n[2] #3 (note) — b\n"
	got := parseIDs(out)
	if len(got) != 2 || got[0] != 7 || got[1] != 3 {
		t.Fatalf("id parse failed: %v", got)
	}
}

func TestTitleClips(t *testing.T) {
	long := "this is a very long note that goes well beyond the sixty character title clip boundary"
	if got := title(long); len([]rune(got)) != 60 {
		t.Fatalf("title should clip to 60 runes, got %d", len([]rune(got)))
	}
	if got := title("first line\nsecond line"); got != "first line" {
		t.Fatalf("title should be the first line, got %q", got)
	}
}
