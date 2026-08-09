package realtime

import (
	"sync"
	"testing"
)

func TestCancelScope_GenZero(t *testing.T) {
	s := NewCancelScope()
	if got := s.Gen(); got != 0 {
		t.Fatalf("initial Gen = %d, want 0", got)
	}
}

func TestCancelScope_CancelIncrements(t *testing.T) {
	s := NewCancelScope()
	if got := s.Cancel(); got != 1 {
		t.Fatalf("first Cancel = %d, want 1", got)
	}
	if got := s.Gen(); got != 1 {
		t.Fatalf("Gen after Cancel = %d, want 1", got)
	}
	if got := s.Cancel(); got != 2 {
		t.Fatalf("second Cancel = %d, want 2", got)
	}
}

func TestCancelScope_IsStale(t *testing.T) {
	tests := []struct {
		name           string
		currentCancels int
		checkGen       int
		wantStale      bool
	}{
		{"current generation not stale", 1, 1, false},
		{"old generation stale", 2, 1, true},
		{"future generation not stale", 1, 2, false},
		{"zero stale after increment", 1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewCancelScope()
			for i := 0; i < tt.currentCancels; i++ {
				s.Cancel()
			}
			if got := s.IsStale(tt.checkGen); got != tt.wantStale {
				t.Fatalf("IsStale(%d) with Gen=%d = %v, want %v", tt.checkGen, s.Gen(), got, tt.wantStale)
			}
		})
	}
}

func TestCancelScope_FlushQueues_PreservesSessionEnd(t *testing.T) {
	tests := []struct {
		name      string
		in        []ServerEvent
		wantTypes []string
	}{
		{
			name: "preserves SESSION_END among deltas",
			in: []ServerEvent{
				{Type: "output_audio.delta", Generation: 1},
				{Type: "transcript.delta", Generation: 1},
				{Type: "SESSION_END", Generation: 1},
				{Type: "response.done", Generation: 1},
			},
			wantTypes: []string{"SESSION_END"},
		},
		{
			name: "drops all without SESSION_END",
			in: []ServerEvent{
				{Type: "output_audio.delta", Generation: 0},
				{Type: "transcript.done", Generation: 0},
			},
			wantTypes: []string{},
		},
		{
			name: "keeps multiple SESSION_END",
			in: []ServerEvent{
				{Type: "SESSION_END", Generation: 2},
				{Type: "SESSION_END", Generation: 3},
				{Type: "output_audio.done", Generation: 3},
			},
			wantTypes: []string{"SESSION_END", "SESSION_END"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := NewCancelScope()
			got := s.FlushQueues(tt.in)
			if len(got) != len(tt.wantTypes) {
				t.Fatalf("FlushQueues = %d events, want %d: got %+v", len(got), len(tt.wantTypes), got)
			}
			for i, want := range tt.wantTypes {
				if got[i].Type != want {
					t.Fatalf("event[%d].Type = %q, want %q", i, got[i].Type, want)
				}
			}
			// ensure no non-SESSION_END leaked
			for _, ev := range got {
				if ev.Type != "SESSION_END" {
					t.Fatalf("FlushQueues leaked non-SESSION_END %q", ev.Type)
				}
			}
		})
	}
}

func TestCancelScope_ThreadSafe(t *testing.T) {
	s := NewCancelScope()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Cancel()
			_ = s.Gen()
			_ = s.IsStale(0)
		}()
	}
	wg.Wait()
	if got := s.Gen(); got != 20 {
		t.Fatalf("concurrent Cancel Gen = %d, want 20", got)
	}
}

func TestCancelScope_ContextCancelledOnCancel(t *testing.T) {
	s := NewCancelScope()
	ctx1 := s.Context()
	s.Cancel()
	select {
	case <-ctx1.Done():
		// expected cancelled
	default:
		t.Fatal("previous context not cancelled after Cancel()")
	}
	ctx2 := s.Context()
	if ctx2 == ctx1 {
		t.Fatal("new context should be different after Cancel")
	}
	select {
	case <-ctx2.Done():
		t.Fatal("new context should not be cancelled yet")
	default:
	}
}
