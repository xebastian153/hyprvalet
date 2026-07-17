// Package ollama implements the core.LLMPort reasoning boundary against a local
// Ollama server. It is an adapter at the edge of the hexagon: the core knows
// only the LLMPort interface, never this HTTP client, the prompt, or the model.
//
// The model never emits shell. It is asked to return a structured object — one
// capability id plus arguments — which the core then validates against the
// allowlist and runs through the same permission gate as a hand-typed call.
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
		http:    &http.Client{Timeout: 60 * time.Second},
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

// intentSchema constrains the model's output to a typed intent via Ollama's
// structured-output support, so the response is always parseable JSON.
var intentSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "capability": {"type": "string"},
    "args": {"type": "object", "additionalProperties": {"type": "string"}},
    "reasoning": {"type": "string"}
  },
  "required": ["capability"]
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

// Interpret asks the model to pick one capability for the request. It satisfies
// core.LLMPort. A transport or HTTP error is returned as an error; a model that
// found no match returns an Intent with an empty Capability (not an error).
func (c *Client) Interpret(ctx context.Context, request string, caps []core.Capability) (core.Intent, error) {
	reqBody := chatRequest{
		Model:  c.model,
		Stream: false,
		Format: intentSchema,
		// Temperature 0: intent mapping should be deterministic, not creative.
		Options: map[string]any{"temperature": 0},
		Messages: []chatMessage{
			{Role: "system", Content: buildSystemPrompt(caps)},
			{Role: "user", Content: request},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return core.Intent{}, fmt.Errorf("encoding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(data))
	if err != nil {
		return core.Intent{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return core.Intent{}, fmt.Errorf("calling ollama at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return core.Intent{}, fmt.Errorf("ollama returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return core.Intent{}, fmt.Errorf("decoding ollama response: %w", err)
	}

	var parsed struct {
		Capability string            `json:"capability"`
		Args       map[string]string `json:"args"`
		Reasoning  string            `json:"reasoning"`
	}
	if err := json.Unmarshal([]byte(cr.Message.Content), &parsed); err != nil {
		return core.Intent{}, fmt.Errorf("model did not return valid intent JSON: %w (got %q)", err, cr.Message.Content)
	}

	return core.Intent{
		Capability: strings.TrimSpace(parsed.Capability),
		Args:       core.Args(parsed.Args),
		Reasoning:  parsed.Reasoning,
	}, nil
}

// buildSystemPrompt describes the allowed capabilities to the model and tells it
// to pick exactly one, or none. The capability list is the model's entire menu:
// it cannot choose anything not here, and the core rejects it if it tries.
func buildSystemPrompt(caps []core.Capability) string {
	var b strings.Builder
	b.WriteString("You translate a user's desktop request into exactly one capability from the list below.\n")
	b.WriteString("Choose the single capability whose action best matches the request and fill its arguments.\n")
	b.WriteString("If no capability matches, return an empty string for \"capability\".\n")
	b.WriteString("Never invent a capability id or an argument name that is not listed. Argument values are strings.\n\n")
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
