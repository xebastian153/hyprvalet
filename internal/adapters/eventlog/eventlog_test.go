package eventlog

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return New(filepath.Join(t.TempDir(), "events.jsonl"))
}

func TestAppendTailRoundTrip(t *testing.T) {
	s := testStore(t)
	at := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	for i, kind := range []core.EventKind{core.EventRan, core.EventDenied, core.EventFailed} {
		err := s.Append(core.Event{
			At:     at.Add(time.Duration(i) * time.Second),
			Source: "cli",
			Kind:   kind,
			Cap:    "a.b",
			Args:   core.Args{"x": "1"},
			Detail: "detail",
		})
		if err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	events, err := s.Tail(0)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	// Oldest first, fields intact.
	if events[0].Kind != core.EventRan || events[2].Kind != core.EventFailed {
		t.Fatalf("order = %v, %v", events[0].Kind, events[2].Kind)
	}
	e := events[0]
	if e.Source != "cli" || e.Cap != "a.b" || e.Args["x"] != "1" || e.Detail != "detail" || !e.At.Equal(at) {
		t.Fatalf("round-trip lost data: %+v", e)
	}
}

func TestTailLimitsToMostRecent(t *testing.T) {
	s := testStore(t)
	for i := 0; i < 5; i++ {
		if err := s.Append(core.Event{At: time.Now(), Kind: core.EventRan, Cap: "a.b", Detail: string(rune('a' + i))}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	events, err := s.Tail(2)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(events) != 2 || events[0].Detail != "d" || events[1].Detail != "e" {
		t.Fatalf("tail(2) = %+v, want the two most recent oldest-first", events)
	}
}

func TestTailMissingLogIsEmpty(t *testing.T) {
	events, err := testStore(t).Tail(10)
	if err != nil || len(events) != 0 {
		t.Fatalf("missing log: events=%v err=%v, want empty and no error", events, err)
	}
}

func TestTailSkipsTornLine(t *testing.T) {
	s := testStore(t)
	if err := s.Append(core.Event{At: time.Now(), Kind: core.EventRan, Cap: "a.b"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// Simulate a crash mid-write: a torn, unparseable final line.
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := f.WriteString(`{"at":"2026-07-17T12:00:00Z","kind":"ran","ca`); err != nil {
		t.Fatalf("write: %v", err)
	}
	f.Close()

	events, err := s.Tail(0)
	if err != nil {
		t.Fatalf("Tail: %v", err)
	}
	if len(events) != 1 || events[0].Cap != "a.b" {
		t.Fatalf("torn line must be skipped, intact ones kept: %+v", events)
	}
}
