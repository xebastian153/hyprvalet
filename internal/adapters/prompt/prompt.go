// Package prompt is the shared language every LLM adapter speaks: the system
// prompts, the JSON schemas, and the parsers that turn model output back into
// core types. Ollama (local) and Groq (cloud) are different transports for the
// SAME contract — keeping that contract in one place is what stops two
// providers from drifting into two dialects.
package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// IntentSchema constrains a single-capability reply to a typed intent. Reply is
// REQUIRED on purpose: small models otherwise put the word "reply" into the
// capability field; forcing both fields to exist makes them fill exactly one.
var IntentSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "capability": {"type": "string"},
    "args": {"type": "object", "additionalProperties": {"type": "string"}},
    "reply": {"type": "string"},
    "reasoning": {"type": "string"}
  },
  "required": ["capability", "reply"]
}`)

// PlanSchema constrains a multi-step reply to an ordered list of typed steps.
// Summary and reply are deliberately NOT required — requiring them makes small
// models empty the steps array.
var PlanSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "summary": {"type": "string"},
    "reply": {"type": "string"},
    "steps": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "capability": {"type": "string"},
          "args": {"type": "object", "additionalProperties": {"type": "string"}}
        },
        "required": ["capability"]
      }
    }
  },
  "required": ["steps"]
}`)

// SpokenLanguage is the language for text that will be spoken aloud (plan
// summaries). It should match the installed TTS voice — HYPRVALET_LANG and
// HYPRVALET_VOICE are two halves of one choice.
func SpokenLanguage() string {
	if v := strings.TrimSpace(os.Getenv("HYPRVALET_LANG")); v != "" {
		return v
	}
	return "English"
}

// ParseIntent turns model output into a core.Intent.
func ParseIntent(content string) (core.Intent, error) {
	var parsed struct {
		Capability string            `json:"capability"`
		Args       map[string]string `json:"args"`
		Reply      string            `json:"reply"`
		Reasoning  string            `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return core.Intent{}, fmt.Errorf("model did not return valid intent JSON: %w (got %q)", err, content)
	}
	return core.Intent{
		Capability: strings.TrimSpace(parsed.Capability),
		Args:       core.Args(parsed.Args),
		Reply:      strings.TrimSpace(parsed.Reply),
		Reasoning:  parsed.Reasoning,
	}, nil
}

// ParsePlan turns model output into a core.Plan for the given request.
func ParsePlan(content, request string) (core.Plan, error) {
	var parsed struct {
		Summary string `json:"summary"`
		Reply   string `json:"reply"`
		Steps   []struct {
			Capability string            `json:"capability"`
			Args       map[string]string `json:"args"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return core.Plan{}, fmt.Errorf("model did not return valid plan JSON: %w (got %q)", err, content)
	}
	steps := make([]core.Step, 0, len(parsed.Steps))
	for _, s := range parsed.Steps {
		steps = append(steps, core.Step{
			Capability: strings.TrimSpace(s.Capability),
			Args:       core.Args(s.Args),
		})
	}
	return core.Plan{Request: request, Summary: parsed.Summary, Reply: strings.TrimSpace(parsed.Reply), Steps: steps}, nil
}

// capabilityList renders the menu the model may choose from: nothing outside it
// is reachable, and the core rejects anything the model invents anyway.
func capabilityList(caps []core.Capability) string {
	var b strings.Builder
	b.WriteString("Capabilities:\n")
	for _, c := range caps {
		params := strings.Join(c.Params(), ", ")
		if params == "" {
			params = "(none)"
		}
		fmt.Fprintf(&b, "- %s: %s [params: %s]\n", c.ID(), c.Description(), params)
	}
	return b.String()
}

// recentActions renders the agent's episodic memory for the system prompt: what
// ran, was refused, or was said lately, so the model can resolve references
// like "again" or "back" against concrete history instead of guessing. Empty
// memory renders nothing — the prompt only grows when there is a past to tell.
func recentActions(recent []core.Event, now time.Time) string {
	if len(recent) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nYour recent actions and conversation, most recent first:\n")
	for i := len(recent) - 1; i >= 0; i-- {
		e := recent[i]
		age := now.Sub(e.At).Round(time.Second)
		if e.Kind == core.EventReplied {
			// Dialogue carries its text — that is what makes it context.
			fmt.Fprintf(&b, "%d. %s ago: %s %s\n", len(recent)-i, age, e.Kind, e.Detail)
			continue
		}
		args := make([]string, 0, len(e.Args))
		for k, v := range e.Args {
			args = append(args, fmt.Sprintf("%s=%s", k, v))
		}
		sort.Strings(args)
		fmt.Fprintf(&b, "%d. %s ago: %s %s %s\n", len(recent)-i, age, e.Kind, e.Cap, strings.Join(args, " "))
	}
	b.WriteString("When the request refers to a past action, resolve it against this history. ")
	b.WriteString("History is context for resolving references — NEVER a suggestion to repeat an action the user did not ask for. ")
	b.WriteString("Every argument value must be a literal value copied from the history or the request ")
	b.WriteString("(a number like 3, a name like firefox) — never a description of one.\n")
	return b.String()
}

// persona is who the assistant IS when it talks: a butler-grade presence —
// composed, concise, quietly witty. Replies are spoken aloud, so brevity is a
// feature, not a style choice.
const persona = "You are hyprvalet, the user's personal voice assistant for this Linux desktop — think of a butler: " +
	"composed, courteous, quietly witty, never verbose. Address the user respectfully (in Spanish, use \"señor\"). " +
	"The user may call you Jarvis; answer to it.\n"

// nowLine gives the model the current date and time so questions like "what
// time is it?" are answered conversationally instead of misrouted to an action.
func nowLine() string {
	return "The current date and time is " + time.Now().Format("Monday 2 January 2006, 15:04") + ".\n"
}

// conversationRule teaches the model the third outcome: talk. A reply is words
// only — the caller speaks it and executes nothing — so the rule insists an
// action is preferred whenever one fits. General knowledge is fair game — the
// user deserves a real assistant, not a menu reader — but facts about THIS
// system that the prompt does not show must never be invented.
// The field contract is spelled out structurally: small models otherwise put
// the word "reply" into the capability field.
const conversationRule = "You always fill exactly one of two fields, never both:\n" +
	"- an action: the capability field holds an id copied from the list (and reply stays \"\").\n" +
	"- an answer: the reply field holds one to three short sentences in the user's language, spoken aloud (and capability stays \"\").\n" +
	"Use an answer when the user greets, thanks, or asks you something instead of requesting an action. " +
	"Answer general questions from your own knowledge, briefly and confidently. " +
	"Never invent facts about this system that you cannot see here; if you do not know, say so. " +
	"Whenever an action fits the request, always prefer the action.\n"

// BuildIntent is the system prompt for single-intent interpretation. The JSON
// shape is spelled out because providers in json_object mode (Groq) see no
// schema parameter — the prompt is the only carrier of the contract there.
func BuildIntent(caps []core.Capability, recent []core.Event) string {
	var b strings.Builder
	b.WriteString(persona)
	b.WriteString(nowLine())
	b.WriteString("You translate a user's desktop request into exactly one capability from the list below.\n")
	b.WriteString("Respond with only a JSON object shaped {\"capability\": \"\", \"args\": {}, \"reply\": \"\", \"reasoning\": \"\"}.\n")
	b.WriteString("Choose the single capability whose action best matches the request and fill its arguments.\n")
	b.WriteString("If no capability matches, return an empty string for \"capability\".\n")
	b.WriteString("Never invent a capability id or an argument name that is not listed. Argument values are strings.\n")
	b.WriteString(conversationRule)
	b.WriteString("\n")
	b.WriteString(capabilityList(caps))
	b.WriteString(recentActions(recent, time.Now()))
	return b.String()
}

// BuildPlan is the system prompt for multi-step planning. The planner only
// plans — conversation is the intent layer's job (callers fall back to it on an
// empty plan).
func BuildPlan(caps []core.Capability, recent []core.Event) string {
	var b strings.Builder
	b.WriteString(persona)
	b.WriteString(nowLine())
	b.WriteString("You turn a user's desktop request into an ordered plan of one or more capability calls from the list below.\n")
	b.WriteString("Respond with only a JSON object shaped {\"summary\": \"\", \"steps\": [{\"capability\": \"\", \"args\": {}}]}.\n")
	b.WriteString("Use as many steps as the request needs, in the order they should run, and fill each step's arguments.\n")
	b.WriteString("Use only capability ids and argument names from the list. If the request cannot be done with these capabilities, return an empty steps array.\n")
	b.WriteString("A greeting, thanks, or a question to you is NOT a desktop action: return an empty steps array for those — never invent a plan for small talk.\n")
	fmt.Fprintf(&b, "Also give a one-line summary of the plan, in %s — it is spoken aloud to the user. ", SpokenLanguage())
	b.WriteString("Each argument value is a plain string with no surrounding braces or quotes — a workspace is 3, not {3} or \"3\".\n\n")
	b.WriteString(capabilityList(caps))
	b.WriteString(recentActions(recent, time.Now()))
	return b.String()
}
