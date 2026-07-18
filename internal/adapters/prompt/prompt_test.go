package prompt

import (
	"context"
	"strings"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// promptCap is a minimal capability for prompt-building assertions.
type promptCap struct{}

func (promptCap) ID() string                                     { return "demo.thing" }
func (promptCap) Description() string                            { return "Do a demo thing" }
func (promptCap) Access() core.AccessKind                        { return core.AccessCommand }
func (promptCap) Risk() core.Risk                                { return core.RiskSafe }
func (promptCap) Params() []string                               { return []string{"widget"} }
func (promptCap) Run(context.Context, core.Args) (string, error) { return "", nil }

func TestPromptsListCapabilities(t *testing.T) {
	caps := []core.Capability{promptCap{}}
	for _, tt := range []struct {
		name   string
		prompt string
	}{
		{"intent prompt", BuildIntent(caps, nil)},
		{"plan prompt", BuildPlan(caps, nil)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.prompt, "demo.thing") {
				t.Error("prompt should list the capability id")
			}
			if !strings.Contains(tt.prompt, "widget") {
				t.Error("prompt should list the capability params")
			}
			// JSON-object providers see no schema parameter: the prompt itself
			// must spell out the JSON shape.
			if !strings.Contains(tt.prompt, "JSON object") {
				t.Error("prompt must describe the JSON shape for schemaless providers")
			}
		})
	}
}

func TestParseIntentRoundTrip(t *testing.T) {
	intent, err := ParseIntent(`{"capability":" a.b ","args":{"x":"1"},"reply":" hi ","reasoning":"r"}`)
	if err != nil {
		t.Fatalf("ParseIntent: %v", err)
	}
	if intent.Capability != "a.b" || intent.Args["x"] != "1" || intent.Reply != "hi" || intent.Reasoning != "r" {
		t.Fatalf("intent = %+v", intent)
	}
	if _, err := ParseIntent("not json"); err == nil {
		t.Fatal("garbage must be an error")
	}
}

func TestParsePlanRoundTrip(t *testing.T) {
	plan, err := ParsePlan(`{"summary":"s","steps":[{"capability":"a.b","args":{"x":"1"}}]}`, "req")
	if err != nil {
		t.Fatalf("ParsePlan: %v", err)
	}
	if plan.Request != "req" || plan.Summary != "s" || len(plan.Steps) != 1 || plan.Steps[0].Args["x"] != "1" {
		t.Fatalf("plan = %+v", plan)
	}
	if _, err := ParsePlan("nope", "req"); err == nil {
		t.Fatal("garbage must be an error")
	}
}
