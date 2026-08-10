package main

import (
	"context"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/adapters/realtime"
	"github.com/xebastian153/hyprvalet/internal/core"
)

type cannedQ struct {
	called *bool
	intent core.Intent
	err    error
}

func (c cannedQ) Interpret(_ context.Context, _ string, _ []core.Capability, _ []core.Event) (core.Intent, error) {
	if c.called != nil {
		*c.called = true
	}
	return c.intent, c.err
}
func (c cannedQ) Plan(_ context.Context, _ string, _ []core.Capability, _ []core.Event) (core.Plan, error) {
	if c.called != nil {
		*c.called = true
	}
	if c.err != nil {
		return core.Plan{}, c.err
	}
	return core.Plan{Summary: "ok"}, nil
}
func (c cannedQ) Chat(_ context.Context, _, _ string) (string, error) {
	if c.called != nil {
		*c.called = true
	}
	if c.err != nil {
		return "", c.err
	}
	return "ok", nil
}

func TestBuildRealtimeReasoner_QuarantineInjectable(t *testing.T) {
	t.Setenv("HYPRVALET_REALTIME", "on")
	orig := realtimeQuarantined
	defer func() { realtimeQuarantined = orig }()
	pc, bc := false, false
	pri := cannedQ{called: &pc, intent: core.Intent{Reasoning: "primary"}}
	bak := cannedQ{called: &bc, intent: core.Intent{Reasoning: "batch"}}
	realtimeQuarantined = func() bool { return true }
	fb := realtime.NewRealtimeFallback(pri, bak, realtimeQuarantined)
	intent, err := fb.Interpret(context.Background(), "x", nil, nil)
	if err != nil || intent.Reasoning != "batch" || pc || !bc {
		t.Fatalf("quarantined should skip primary: pc=%v bc=%v intent=%+v err=%v", pc, bc, intent, err)
	}
	pc, bc = false, false
	realtimeQuarantined = func() bool { return false }
	fb2 := realtime.NewRealtimeFallback(pri, bak, realtimeQuarantined)
	intent2, err := fb2.Interpret(context.Background(), "x", nil, nil)
	if err != nil || intent2.Reasoning != "primary" || !pc {
		t.Fatalf("not quarantined should use primary: pc=%v intent=%+v err=%v", pc, intent2, err)
	}
	if buildRealtimeReasoner(false, false) == nil {
		t.Fatal("buildRealtimeReasoner returned nil")
	}
}
