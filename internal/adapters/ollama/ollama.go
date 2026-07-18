// Package ollama implements the core reasoning ports (LLMPort and PlannerPort)
// against a local Ollama server. It is an adapter at the edge of the hexagon:
// the core knows only the interfaces, never this HTTP client, the prompt, or the
// model.
//
// The model never emits shell. It returns a structured object — one capability
// (LLMPort) or an ordered list of capability steps (PlannerPort) — which the
// core validates against the allowlist and runs through the same permission gate
// as a hand-typed call.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Client talks to an Ollama server's /api/chat endpoint.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// New returns a client for a specific server URL and model. Tests inject a mock
// server URL here; production uses Default.
func New(baseURL, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

// Default builds a client from the environment, falling back to a local Ollama
// and a model good at structured output. Override with HYPRVALET_OLLAMA_URL and
// HYPRVALET_MODEL.
func Default() *Client {
	return New(
		envOr("HYPRVALET_OLLAMA_URL", "http://localhost:11434"),
		envOr("HYPRVALET_MODEL", "qwen2.5:7b"),
	)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// intentSchema constrains a single-capability reply to a typed intent.
var intentSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "capability": {"type": "string"},
    "args": {"type": "object", "additionalProperties": {"type": "string"}},
    "reasoning": {"type": "string"}
  },
  "required": ["capability"]
}`)

// planSchema constrains a multi-step reply to an ordered list of typed steps.
var planSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "summary": {"type": "string"},
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

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string          `json:"model"`
	Messages []chatMessage   `json:"messages"`
	Stream   bool            `json:"stream"`
	Format   json.RawMessage `json:"format,omitempty"`
	Options  map[string]any  `json:"options,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
}

// chat posts a system+user exchange constrained to a JSON schema and returns the
// model's raw message content. Temperature 0 keeps mapping deterministic rather
// than creative. Interpret and Plan share it, differing only in schema/prompt
// and how they parse the content.
func (c *Client) chat(ctx context.Context, system, user string, format json.RawMessage) (string, error) {
	reqBody := chatRequest{
		Model:   c.model,
		Stream:  false,
		Format:  format,
		Options: map[string]any{"temperature": 0},
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("encoding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("calling ollama at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", fmt.Errorf("decoding ollama response: %w", err)
	}
	return cr.Message.Content, nil
}

// Interpret maps a request to one capability. It satisfies core.LLMPort.
func (c *Client) Interpret(ctx context.Context, request string, caps []core.Capability) (core.Intent, error) {
	content, err := c.chat(ctx, buildIntentPrompt(caps), request, intentSchema)
	if err != nil {
		return core.Intent{}, err
	}
	var parsed struct {
		Capability string            `json:"capability"`
		Args       map[string]string `json:"args"`
		Reasoning  string            `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return core.Intent{}, fmt.Errorf("model did not return valid intent JSON: %w (got %q)", err, content)
	}
	return core.Intent{
		Capability: strings.TrimSpace(parsed.Capability),
		Args:       core.Args(parsed.Args),
		Reasoning:  parsed.Reasoning,
	}, nil
}

// Plan maps a request to an ordered plan of capability steps. It satisfies
// core.PlannerPort. A request the model cannot fulfill returns an empty plan.
func (c *Client) Plan(ctx context.Context, request string, caps []core.Capability) (core.Plan, error) {
	content, err := c.chat(ctx, buildPlanPrompt(caps), request, planSchema)
	if err != nil {
		return core.Plan{}, err
	}
	var parsed struct {
		Summary string `json:"summary"`
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
	return core.Plan{Request: request, Summary: parsed.Summary, Steps: steps}, nil
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

func buildIntentPrompt(caps []core.Capability) string {
	var b strings.Builder
	b.WriteString("You translate a user's desktop request into exactly one capability from the list below.\n")
	b.WriteString("Choose the single capability whose action best matches the request and fill its arguments.\n")
	b.WriteString("If no capability matches, return an empty string for \"capability\".\n")
	b.WriteString("Never invent a capability id or an argument name that is not listed. Argument values are strings.\n\n")
	b.WriteString(capabilityList(caps))
	return b.String()
}

func buildPlanPrompt(caps []core.Capability) string {
	var b strings.Builder
	b.WriteString("You turn a user's desktop request into an ordered plan of one or more capability calls from the list below.\n")
	b.WriteString("Use as many steps as the request needs, in the order they should run, and fill each step's arguments.\n")
	b.WriteString("Use only capability ids and argument names from the list. If the request cannot be done with these capabilities, return an empty steps array.\n")
	b.WriteString("Also give a one-line summary of the plan. Each argument value is a plain string with no surrounding braces or quotes — a workspace is 3, not {3} or \"3\".\n\n")
	b.WriteString(capabilityList(caps))
	return b.String()
}
