package fallback

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// canned is a Reasoner returning fixed results or errors.
type canned struct {
	intent core.Intent
	plan   core.Plan
	err    error
}

func (c canned) Interpret(context.Context, string, []core.Capability, []core.Event) (core.Intent, error) {
	return c.intent, c.err
}
func (c canned) Plan(context.Context, string, []core.Capability, []core.Event) (core.Plan, error) {
	return c.plan, c.err
}

func TestPrimaryWinsWhenHealthy(t *testing.T) {
	c := New(
		canned{intent: core.Intent{Reasoning: "primary"}},
		canned{intent: core.Intent{Reasoning: "backup"}},
	)
	intent, err := c.Interpret(context.Background(), "x", nil, nil)
	if err != nil || intent.Reasoning != "primary" {
		t.Fatalf("intent = %+v err=%v, want primary", intent, err)
	}
}

func TestBackupTakesOverOnPrimaryFailure(t *testing.T) {
	c := New(
		canned{err: errors.New("network is down")},
		canned{intent: core.Intent{Reasoning: "backup"}, plan: core.Plan{Summary: "backup"}},
	)
	intent, err := c.Interpret(context.Background(), "x", nil, nil)
	if err != nil || intent.Reasoning != "backup" {
		t.Fatalf("intent = %+v err=%v, want backup", intent, err)
	}
	plan, err := c.Plan(context.Background(), "x", nil, nil)
	if err != nil || plan.Summary != "backup" {
		t.Fatalf("plan = %+v err=%v, want backup", plan, err)
	}
}

func TestBothFailingReportsBoth(t *testing.T) {
	c := New(
		canned{err: errors.New("cloud down")},
		canned{err: errors.New("local down")},
	)
	_, err := c.Interpret(context.Background(), "x", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "cloud down") || !strings.Contains(err.Error(), "local down") {
		t.Fatalf("err = %v, want both failures reported", err)
	}
}
