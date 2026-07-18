package core

import (
	"context"
	"testing"
)

// stubPlanner is a canned PlannerPort — proof the interface is implementable.
type stubPlanner struct {
	plan Plan
	err  error
}

func (s stubPlanner) Plan(context.Context, string, []Capability) (Plan, error) {
	return s.plan, s.err
}

var _ PlannerPort = stubPlanner{}

func planTestRegistry() *Registry {
	reg := NewRegistry()
	for _, id := range []string{"workspace.switch", "app.open", "omarchy.run"} {
		_ = reg.Register(fakeCap{id: id})
	}
	return reg
}

func TestPlanValidate(t *testing.T) {
	reg := planTestRegistry()
	tests := []struct {
		name    string
		plan    Plan
		wantErr bool
	}{
		{
			"valid multi-step plan",
			Plan{Steps: []Step{
				{Capability: "workspace.switch", Args: Args{"workspace": "2"}},
				{Capability: "app.open", Args: Args{"cmd": "code"}},
			}},
			false,
		},
		{"no steps", Plan{}, true},
		{
			"unknown capability rejected by the allowlist",
			Plan{Steps: []Step{{Capability: "system.wipe"}}},
			true,
		},
		{
			"lifecycle guard violation",
			Plan{Steps: []Step{{Capability: "omarchy.run", Args: Args{"args": "killall Hyprland"}}}},
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plan.Validate(reg)
			if tt.wantErr && err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestPlannerPortContract(t *testing.T) {
	planner := stubPlanner{err: context.DeadlineExceeded}
	if _, err := planner.Plan(context.Background(), "x", nil); err == nil {
		t.Fatal("stub should propagate its error")
	}

	planner = stubPlanner{plan: Plan{Steps: nil}}
	got, err := planner.Plan(context.Background(), "impossible", nil)
	if err != nil {
		t.Fatalf("an unfulfillable request must not be an error: %v", err)
	}
	if len(got.Steps) != 0 {
		t.Fatalf("expected an empty plan, got %d steps", len(got.Steps))
	}
}
