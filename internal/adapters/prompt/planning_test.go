package prompt

import (
	"strings"
	"testing"
)

func TestParsePlanningQuestion(t *testing.T) {
	turn, err := ParsePlanning(`{"question": "¿Para qué es el proyecto?"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.Plan != nil {
		t.Fatal("a question turn must not carry a plan")
	}
	if turn.Question != "¿Para qué es el proyecto?" {
		t.Fatalf("question wrong: %q", turn.Question)
	}
}

func TestParsePlanningPlan(t *testing.T) {
	raw := `{"plan": {"name": "Tienda", "goal": "vender productos online", "stack": ["Go", "Postgres"], "features": ["catálogo", "carrito"], "first_steps": ["definir modelos"]}}`
	turn, err := ParsePlanning(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if turn.Plan == nil {
		t.Fatal("expected a plan")
	}
	if turn.Plan.Name != "Tienda" || len(turn.Plan.Stack) != 2 {
		t.Fatalf("plan parsed wrong: %+v", turn.Plan)
	}
}

func TestParsePlanningPrefersNamedPlanOverQuestion(t *testing.T) {
	// A reply with both must resolve to the plan when it is named.
	raw := `{"question": "leftover", "plan": {"name": "X", "goal": "y"}}`
	turn, _ := ParsePlanning(raw)
	if turn.Plan == nil || turn.Question != "" {
		t.Fatalf("named plan should win: %+v", turn)
	}
}

func TestParsePlanningEmptyPlanFallsToQuestion(t *testing.T) {
	// An unnamed plan is not usable; a present question should be used instead.
	raw := `{"question": "still need a name?", "plan": {"name": "", "goal": ""}}`
	turn, err := ParsePlanning(raw)
	if err != nil || turn.Plan != nil || turn.Question != "still need a name?" {
		t.Fatalf("should fall back to question: %+v err=%v", turn, err)
	}
}

func TestParsePlanningBareStringIsQuestion(t *testing.T) {
	turn, err := ParsePlanning("What are we building?")
	if err != nil || turn.Question != "What are we building?" {
		t.Fatalf("bare text should be a question: %+v err=%v", turn, err)
	}
}

func TestParsePlanningEmptyIsError(t *testing.T) {
	if _, err := ParsePlanning("   "); err == nil {
		t.Fatal("empty reply must error")
	}
}

func TestPlanNoteAndHandoffCarryEssentials(t *testing.T) {
	p := ProjectPlan{
		Name: "Tienda", Goal: "vender online",
		Stack: []string{"Go"}, Features: []string{"carrito"},
		FirstSteps: []string{"modelos"},
	}
	for _, s := range []string{p.Note(), p.Handoff()} {
		for _, want := range []string{"Tienda", "vender online", "Go", "carrito"} {
			if !strings.Contains(s, want) {
				t.Fatalf("rendering %q missing %q", s, want)
			}
		}
	}
}
