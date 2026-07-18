// Package ollama implements the core reasoning ports (LLMPort and PlannerPort)
// against a local Ollama server. It is an adapter at the edge of the hexagon:
// the core knows only the interfaces, never this HTTP client or the model. The
// prompts and parsers it speaks live in the shared prompt package — Ollama is
// one transport for that contract, Groq is another.
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

	"github.com/xebastian153/hyprvalet/internal/adapters/prompt"
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

// Strong builds the escalation client: the larger model the corrective loop
// falls back to when the default model cannot fix its own mistake. Override
// with HYPRVALET_MODEL_STRONG. Same server, different depth — the HYBRID
// design's second tier.
func Strong() *Client {
	return New(
		envOr("HYPRVALET_OLLAMA_URL", "http://localhost:11434"),
		envOr("HYPRVALET_MODEL_STRONG", "qwen2.5:7b"),
	)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

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

// Chat is a raw JSON chat turn for higher-level conversational flows (project
// planning) that are not typed capability calls. Ollama's generic JSON mode is
// used; the prompt carries the exact shape.
func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	return c.chat(ctx, system, user, json.RawMessage(`"json"`))
}

// Interpret maps a request to one capability. It satisfies core.LLMPort.
func (c *Client) Interpret(ctx context.Context, request string, caps []core.Capability, recent []core.Event) (core.Intent, error) {
	content, err := c.chat(ctx, prompt.BuildIntent(caps, recent), request, prompt.IntentSchema)
	if err != nil {
		return core.Intent{}, err
	}
	return prompt.ParseIntent(content)
}

// Plan maps a request to an ordered plan of capability steps. It satisfies
// core.PlannerPort. A request the model cannot fulfill returns an empty plan.
func (c *Client) Plan(ctx context.Context, request string, caps []core.Capability, recent []core.Event) (core.Plan, error) {
	content, err := c.chat(ctx, prompt.BuildPlan(caps, recent), request, prompt.PlanSchema)
	if err != nil {
		return core.Plan{}, err
	}
	return prompt.ParsePlan(content, request)
}
