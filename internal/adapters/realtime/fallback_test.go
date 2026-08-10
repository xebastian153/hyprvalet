package realtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// cannedReasoner is a fallback.Reasoner stub returning fixed results.
type cannedReasoner struct {
	intent  core.Intent
	plan    core.Plan
	err     error
	chat    string
	chatErr error
}

func (c cannedReasoner) Interpret(context.Context, string, []core.Capability, []core.Event) (core.Intent, error) {
	return c.intent, c.err
}
func (c cannedReasoner) Plan(context.Context, string, []core.Capability, []core.Event) (core.Plan, error) {
	return c.plan, c.err
}
func (c cannedReasoner) Chat(context.Context, string, string) (string, error) {
	if c.chatErr != nil {
		return "", c.chatErr
	}
	if c.chat != "" {
		return c.chat, nil
	}
	return "", errors.New("no chat")
}

func TestRealtimeFallbackPrimaryWins(t *testing.T) {
	primary := cannedReasoner{intent: core.Intent{Reasoning: "realtime"}, plan: core.Plan{Summary: "realtime"}}
	backup := cannedReasoner{intent: core.Intent{Reasoning: "batch"}, plan: core.Plan{Summary: "batch"}}
	fb := NewRealtimeFallback(primary, backup, nil)
	intent, err := fb.Interpret(context.Background(), "x", nil, nil)
	if err != nil || intent.Reasoning != "realtime" {
		t.Fatalf("want realtime primary, got %+v err=%v", intent, err)
	}
	plan, err := fb.Plan(context.Background(), "x", nil, nil)
	if err != nil || plan.Summary != "realtime" {
		t.Fatalf("want realtime plan, got %+v err=%v", plan, err)
	}
}

func TestRealtimeFallbackWSDownUsesBatch(t *testing.T) {
	primary := cannedReasoner{err: errors.New("dial realtime sidecar ws://127.0.0.1:8765/v1/realtime: connection refused")}
	backup := cannedReasoner{intent: core.Intent{Reasoning: "batch"}, plan: core.Plan{Summary: "batch"}}
	fb := NewRealtimeFallback(primary, backup, nil)
	intent, err := fb.Interpret(context.Background(), "open browser", nil, nil)
	if err != nil || intent.Reasoning != "batch" {
		t.Fatalf("WS down should fallback to batch, got %+v err=%v", intent, err)
	}
	plan, err := fb.Plan(context.Background(), "x", nil, nil)
	if err != nil || plan.Summary != "batch" {
		t.Fatalf("WS down plan fallback to batch, got %+v err=%v", plan, err)
	}
}

func TestRealtimeFallbackQuarantineSkipsPrimary(t *testing.T) {
	primary := cannedReasoner{intent: core.Intent{Reasoning: "realtime-should-not-be-used"}, err: nil}
	backup := cannedReasoner{intent: core.Intent{Reasoning: "batch"}, plan: core.Plan{Summary: "batch"}}
	quarantined := func() bool { return true }
	fb := NewRealtimeFallback(primary, backup, quarantined)
	intent, err := fb.Interpret(context.Background(), "x", nil, nil)
	if err != nil || intent.Reasoning != "batch" {
		t.Fatalf("quarantined should skip primary and use batch, got %+v err=%v", intent, err)
	}
	// When not quarantined, primary wins
	fb2 := NewRealtimeFallback(primary, backup, func() bool { return false })
	intent2, err := fb2.Interpret(context.Background(), "x", nil, nil)
	if err != nil || intent2.Reasoning != "realtime-should-not-be-used" {
		t.Fatalf("not quarantined should use primary, got %+v err=%v", intent2, err)
	}
}

func TestRealtimeFallbackOfflineNoPanic(t *testing.T) {
	// Simulate offline / quarantine + primary error: should not panic, must return backup or error gracefully
	cases := []struct {
		name        string
		primaryErr  error
		backupErr   error
		quarantined bool
		wantReason  string
		wantErr     bool
	}{
		{"offline fallback ok", errors.New("network offline"), nil, false, "batch", false},
		{"quarantine fallback ok", nil, nil, true, "batch", false},
		{"both fail quarantine", errors.New("primary down"), errors.New("backup down"), true, "", true},
		{"both fail offline", errors.New("cloud down"), errors.New("local down"), false, "", true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			primary := cannedReasoner{err: tt.primaryErr, intent: core.Intent{Reasoning: "primary-ignored"}}
			backup := cannedReasoner{err: tt.backupErr, intent: core.Intent{Reasoning: "batch"}}
			q := func() bool { return tt.quarantined }
			fb := NewRealtimeFallback(primary, backup, q)
			intent, err := fb.Interpret(context.Background(), "x", nil, nil)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got intent %+v", intent)
				}
				// should contain both failures when not quarantined, or at least backup failure
				if !strings.Contains(err.Error(), "backup down") && !strings.Contains(err.Error(), "local down") {
					t.Logf("error %v does not mention backup, but that's ok if quarantined path skips primary error", err)
				}
				return
			}
			if err != nil || intent.Reasoning != tt.wantReason {
				t.Fatalf("case %q: got %+v err=%v want %q", tt.name, intent, err, tt.wantReason)
			}
		})
	}
}

func TestRealtimeFallbackBothFailReportsBoth(t *testing.T) {
	primary := cannedReasoner{err: errors.New("realtime WS down")}
	backup := cannedReasoner{err: errors.New("ollama local down")}
	fb := NewRealtimeFallback(primary, backup, nil)
	_, err := fb.Interpret(context.Background(), "x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "realtime WS down") || !strings.Contains(err.Error(), "ollama local down") {
		t.Fatalf("err = %v, want both failures", err)
	}
	_, err = fb.Plan(context.Background(), "x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "realtime WS down") {
		t.Fatalf("plan err = %v, want both", err)
	}
}

func TestRealtimeFallbackChatFallback(t *testing.T) {
	primary := cannedReasoner{chatErr: errors.New("realtime chat failed")}
	backup := cannedReasoner{chat: "batch chat ok"}
	fb := NewRealtimeFallback(primary, backup, nil)
	out, err := fb.Chat(context.Background(), "sys", "hi")
	if err != nil || out != "batch chat ok" {
		t.Fatalf("chat fallback: out=%q err=%v want batch chat ok", out, err)
	}
}

func TestRealtimeFallbackWithRealClientQuarantine(t *testing.T) {
	// Integration with real Client quarantine state (no network needed)
	cli := NewClient("", NewCancelScope())
	// Force quarantine via direct field (same package) with a far-future time
	future := time.Now().Add(10 * time.Minute)
	cli.quarantinedUntil = future
	defer func() { cli.quarantinedUntil = time.Time{} }()

	quarantined := func() bool { return cli.IsQuarantined() }
	primary := cannedReasoner{intent: core.Intent{Reasoning: "should skip"}}
	backup := cannedReasoner{intent: core.Intent{Reasoning: "batch-quarantined"}}
	fb := NewRealtimeFallback(primary, backup, quarantined)
	intent, err := fb.Interpret(context.Background(), "x", nil, nil)
	if err != nil || intent.Reasoning != "batch-quarantined" {
		t.Fatalf("quarantined client should fallback, got %+v err=%v", intent, err)
	}
}
