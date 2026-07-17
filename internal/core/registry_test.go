package core

import (
	"context"
	"testing"
)

// fakeCap is a minimal Capability stand-in for registry tests — the registry
// only cares about the interface, never the concrete adapter.
type fakeCap struct {
	id string
}

func (f fakeCap) ID() string                              { return f.id }
func (fakeCap) Description() string                       { return "fake" }
func (fakeCap) Access() AccessKind                        { return AccessCommand }
func (fakeCap) Risk() Risk                                { return RiskSafe }
func (fakeCap) Params() []string                          { return nil }
func (fakeCap) Run(context.Context, Args) (string, error) { return "", nil }

func TestRegistryRegister(t *testing.T) {
	t.Run("registers a new capability", func(t *testing.T) {
		r := NewRegistry()
		if err := r.Register(fakeCap{id: "a.one"}); err != nil {
			t.Fatalf("Register returned error on first insert: %v", err)
		}
	})

	t.Run("rejects a duplicate ID so adapters cannot shadow each other", func(t *testing.T) {
		r := NewRegistry()
		if err := r.Register(fakeCap{id: "a.one"}); err != nil {
			t.Fatalf("unexpected error on first insert: %v", err)
		}
		if err := r.Register(fakeCap{id: "a.one"}); err == nil {
			t.Fatal("Register accepted a duplicate ID; the allowlist must reject it")
		}
	})
}

func TestRegistryGetIsAnAllowlist(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeCap{id: "a.one"}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	t.Run("returns a registered capability", func(t *testing.T) {
		got, ok := r.Get("a.one")
		if !ok {
			t.Fatal("Get reported a registered capability as missing")
		}
		if got.ID() != "a.one" {
			t.Fatalf("Get returned wrong capability: %q", got.ID())
		}
	})

	t.Run("reports an unregistered capability as impossible", func(t *testing.T) {
		if _, ok := r.Get("never.registered"); ok {
			t.Fatal("Get resolved an unregistered ID; anything not registered must be impossible")
		}
	})
}

func TestRegistryListIsSorted(t *testing.T) {
	r := NewRegistry()
	for _, id := range []string{"c.three", "a.one", "b.two"} {
		if err := r.Register(fakeCap{id: id}); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	got := r.List()
	want := []string{"a.one", "b.two", "c.three"}
	if len(got) != len(want) {
		t.Fatalf("List returned %d capabilities, want %d", len(got), len(want))
	}
	for i, c := range got {
		if c.ID() != want[i] {
			t.Fatalf("List not sorted: at index %d got %q, want %q", i, c.ID(), want[i])
		}
	}
}
