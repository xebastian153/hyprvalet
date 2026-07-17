package core

import (
	"context"
	"testing"
)

// stubLLM is a canned LLMPort — proof the interface is implementable and a stand
// -in for wiring tests without a real model.
type stubLLM struct {
	intent Intent
	err    error
}

func (s stubLLM) Interpret(context.Context, string, []Capability) (Intent, error) {
	return s.intent, s.err
}

// Compile-time check that the stub satisfies the port.
var _ LLMPort = stubLLM{}

func TestResolveIntent(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(fakeCap{id: "workspace.switch"}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Run("resolves a registered capability", func(t *testing.T) {
		got, err := ResolveIntent(reg, Intent{Capability: "workspace.switch", Args: Args{"workspace": "3"}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.ID() != "workspace.switch" {
			t.Fatalf("resolved wrong capability: %q", got.ID())
		}
	})

	t.Run("rejects an empty capability (no match)", func(t *testing.T) {
		if _, err := ResolveIntent(reg, Intent{Capability: ""}); err == nil {
			t.Fatal("empty capability should be an error")
		}
	})

	t.Run("rejects a hallucinated capability via the allowlist", func(t *testing.T) {
		if _, err := ResolveIntent(reg, Intent{Capability: "system.format_disk"}); err == nil {
			t.Fatal("an unregistered capability must be rejected, not resolved")
		}
	})
}

func TestLLMPortContract(t *testing.T) {
	// A returned error surfaces as a reasoning failure.
	llm := stubLLM{err: context.DeadlineExceeded}
	if _, err := llm.Interpret(context.Background(), "anything", nil); err == nil {
		t.Fatal("expected the stub to propagate its error")
	}

	// A successful "no match" is an empty Intent, not an error.
	llm = stubLLM{intent: Intent{Capability: ""}}
	got, err := llm.Interpret(context.Background(), "do something impossible", nil)
	if err != nil {
		t.Fatalf("no-match must not be an error: %v", err)
	}
	if got.Capability != "" {
		t.Fatalf("expected empty capability, got %q", got.Capability)
	}
}
