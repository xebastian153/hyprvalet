package core

import (
	"testing"
	"time"
)

func TestRecentEventsFiltersByWindow(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	events := []Event{
		{Cap: "old", At: now.Add(-2 * time.Hour)},
		{Cap: "edge", At: now.Add(-time.Hour)}, // exactly at the window: kept
		{Cap: "new", At: now.Add(-time.Minute)},
	}
	got := RecentEvents(events, now, time.Hour)
	if len(got) != 2 || got[0].Cap != "edge" || got[1].Cap != "new" {
		t.Fatalf("RecentEvents = %+v, want edge and new in order", got)
	}
	if RecentEvents(nil, now, time.Hour) != nil {
		t.Fatal("no events must stay no events")
	}
}
