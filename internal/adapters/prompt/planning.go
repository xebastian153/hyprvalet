package prompt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxPlanQuestions caps how many questions the assistant asks before it must
// commit to a plan — a friend helps you decide, they don't interrogate you.
const maxPlanQuestions = 5

// minPlanQuestions is how many the assistant asks BEFORE it may write a plan.
// Without this a confident model skips the conversation and invents a plan from
// the one-line idea — the opposite of thinking it through together.
const minPlanQuestions = 2

// ProjectPlan is the outcome of a planning conversation: enough shape to hand a
// project to Claude Code and to remember what was decided.
type ProjectPlan struct {
	Name       string   `json:"name"`
	Goal       string   `json:"goal"`
	Stack      []string `json:"stack"`
	Features   []string `json:"features"`
	FirstSteps []string `json:"first_steps"`
}

// PlanningTurn is one step of the conversation: either the assistant asks the
// next question, or it has enough and returns the finished plan. Exactly one is
// set.
type PlanningTurn struct {
	Question string
	Plan     *ProjectPlan
}

// BuildPlanning is the system prompt for the planning conversation. The
// assistant behaves like a thoughtful friend helping shape an idea: it asks one
// short question at a time, and once it knows the essentials it stops asking and
// writes the plan.
func BuildPlanning(asked int) string {
	var b strings.Builder
	b.WriteString(persona)
	b.WriteString(nowLine())
	b.WriteString("You are helping the user plan a software project, like a friend thinking it through with them — warm, curious, concrete.\n")
	b.WriteString("Work ONE step at a time. Respond with only a JSON object in one of these two shapes:\n")
	b.WriteString("  {\"question\": \"...\"}   when you need to know more — ONE short, friendly question in the user's language.\n")
	b.WriteString("  {\"plan\": {\"name\": \"\", \"goal\": \"\", \"stack\": [], \"features\": [], \"first_steps\": []}}   when you have enough to start.\n")
	b.WriteString("Ask about what matters to actually build it: the goal, who it is for, the language or stack, the key features, the scope of a first version.\n")
	b.WriteString("Ask only what you do not already know from the conversation — never repeat a question that was answered.\n")
	fmt.Fprintf(&b, "You have already asked %d question(s). ", asked)
	switch {
	case asked >= maxPlanQuestions:
		b.WriteString("You have reached the limit — return the plan now, filling any gaps with sensible defaults.\n")
	case asked < minPlanQuestions:
		fmt.Fprintf(&b, "You do NOT yet know enough — you must ask a question now (you must ask at least %d before planning). Return a {\"question\": ...} object; do NOT return a plan yet.\n", minPlanQuestions)
	default:
		b.WriteString("Ask another question if a real gap remains, otherwise return the plan. Never invent details the user did not give — if the stack or key features are still unknown, ask.\n")
	}
	fmt.Fprintf(&b, "In the plan, write name/goal in the user's language; keep stack items as plain technology names. The goal is one sentence. Give 3 to 6 features and 3 to 6 first_steps, each a short phrase. Reply in %s when you ask questions.\n", SpokenLanguage())
	return b.String()
}

// ParsePlanning turns a planning reply into a PlanningTurn. It is defensive:
// small models wrap or stringify things, so a plan with a name wins, otherwise a
// question, and a bare string is treated as a question.
func ParsePlanning(content string) (PlanningTurn, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return PlanningTurn{}, fmt.Errorf("empty planning reply")
	}
	var parsed struct {
		Question string       `json:"question"`
		Plan     *ProjectPlan `json:"plan"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		// Not JSON — take the whole thing as the next question.
		return PlanningTurn{Question: content}, nil
	}
	if parsed.Plan != nil && strings.TrimSpace(parsed.Plan.Name) != "" {
		return PlanningTurn{Plan: parsed.Plan}, nil
	}
	if q := strings.TrimSpace(parsed.Question); q != "" {
		return PlanningTurn{Question: q}, nil
	}
	return PlanningTurn{}, fmt.Errorf("planning reply had neither a question nor a named plan")
}

// Summary renders a plan as a short spoken paragraph — what the assistant reads
// back so the user hears what was decided.
func (p ProjectPlan) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", strings.TrimSpace(p.Name), strings.TrimSpace(p.Goal))
	if len(p.Stack) > 0 {
		fmt.Fprintf(&b, " Stack: %s.", strings.Join(p.Stack, ", "))
	}
	if len(p.Features) > 0 {
		fmt.Fprintf(&b, " Key features: %s.", strings.Join(p.Features, ", "))
	}
	return b.String()
}

// Note renders a plan as one durable memory line, so recall can surface it later
// ("what was the plan for the shop?").
func (p ProjectPlan) Note() string {
	parts := []string{"Project plan — " + strings.TrimSpace(p.Name) + ": " + strings.TrimSpace(p.Goal)}
	if len(p.Stack) > 0 {
		parts = append(parts, "stack: "+strings.Join(p.Stack, ", "))
	}
	if len(p.Features) > 0 {
		parts = append(parts, "features: "+strings.Join(p.Features, ", "))
	}
	if len(p.FirstSteps) > 0 {
		parts = append(parts, "first steps: "+strings.Join(p.FirstSteps, ", "))
	}
	return strings.Join(parts, "; ")
}

// Handoff renders the plan as the message the assistant relays to Claude Code to
// kick off the build.
func (p ProjectPlan) Handoff() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Let's build %s. Goal: %s.", strings.TrimSpace(p.Name), strings.TrimSpace(p.Goal))
	if len(p.Stack) > 0 {
		fmt.Fprintf(&b, " Stack: %s.", strings.Join(p.Stack, ", "))
	}
	if len(p.Features) > 0 {
		fmt.Fprintf(&b, " Features: %s.", strings.Join(p.Features, "; "))
	}
	if len(p.FirstSteps) > 0 {
		fmt.Fprintf(&b, " First steps: %s.", strings.Join(p.FirstSteps, "; "))
	}
	b.WriteString(" Ask me if anything is unclear before you start.")
	return b.String()
}
