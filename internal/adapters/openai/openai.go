// Package openai implements the core reasoning ports (LLMPort and PlannerPort)
// against OpenAI's chat-completions API. It speaks the same shared prompt
// contract as the other reasoning adapters — OpenAI's API is the dialect Groq
// and others emulate, so only the endpoint, key, and model names differ.
//
// Cost note: for a voice assistant's short commands, a small model like
// gpt-4o-mini costs a fraction of a cent per request — far less than a monthly
// subscription for this workload. Privacy is the explicit trade: every request,
// including the episodic memory rendered into the prompt, leaves the machine.
// The caller composes this client with a local fallback so losing the network
// never silences the agent.
package openai

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

// Client talks to an OpenAI-compatible chat-completions endpoint.
type Client struct {
	baseURL string
	model   string
	key     string
	http    *http.Client
}

// Available reports whether OpenAI can be used: an API key is configured.
func Available() bool {
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != ""
}

// New returns a client for a specific endpoint, model, and key. Tests inject a
// mock server URL here; production uses Default.
func New(baseURL, model, key string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		key:     key,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
}

// Default builds a client from the environment. Override the model with
// HYPRVALET_OPENAI_MODEL and the endpoint with HYPRVALET_OPENAI_URL. The default
// model is small and cheap — the right tier for short spoken commands.
func Default() *Client {
	return New(
		envOr("HYPRVALET_OPENAI_URL", "https://api.openai.com/v1"),
		envOr("HYPRVALET_OPENAI_MODEL", "gpt-4o-mini"),
		os.Getenv("OPENAI_API_KEY"),
	)
}

// Strong builds the escalation client — a larger model for the corrective retry
// when the small one mis-mapped. HYPRVALET_OPENAI_MODEL_STRONG overrides it.
func Strong() *Client {
	return New(
		envOr("HYPRVALET_OPENAI_URL", "https://api.openai.com/v1"),
		envOr("HYPRVALET_OPENAI_MODEL_STRONG", "gpt-4o"),
		os.Getenv("OPENAI_API_KEY"),
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
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat responseFormat `json:"response_format"`
}

// responseFormat requests JSON-object mode: the model must emit a single JSON
// object. It does not constrain the SHAPE — the shared prompt spells that out,
// and the parse/allowlist/validation layers catch anything that drifts.
type responseFormat struct {
	Type string `json:"type"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
}

// chat posts a system+user exchange in JSON-object mode and returns the model's
// raw message content. Temperature 0 keeps mapping deterministic.
func (c *Client) chat(ctx context.Context, system, user string) (string, error) {
	if c.key == "" {
		return "", fmt.Errorf("OPENAI_API_KEY is not set — get one at https://platform.openai.com and export it")
	}
	reqBody := chatRequest{
		Model:          c.model,
		Temperature:    0,
		ResponseFormat: responseFormat{Type: "json_object"},
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("encoding request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+c.key)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("calling openai at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}

	var cr chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		return "", fmt.Errorf("decoding openai response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("openai returned no choices")
	}
	return cr.Choices[0].Message.Content, nil
}

// Chat is a raw JSON chat turn for higher-level conversational flows (project
// planning) that are not typed capability calls.
func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	return c.chat(ctx, system, user)
}

// Interpret maps a request to one capability. It satisfies core.LLMPort.
func (c *Client) Interpret(ctx context.Context, request string, caps []core.Capability, recent []core.Event) (core.Intent, error) {
	content, err := c.chat(ctx, prompt.BuildIntent(caps, recent), request)
	if err != nil {
		return core.Intent{}, err
	}
	return prompt.ParseIntent(content)
}

// Plan maps a request to an ordered plan of capability steps. It satisfies
// core.PlannerPort.
func (c *Client) Plan(ctx context.Context, request string, caps []core.Capability, recent []core.Event) (core.Plan, error) {
	content, err := c.chat(ctx, prompt.BuildPlan(caps, recent), request)
	if err != nil {
		return core.Plan{}, err
	}
	return prompt.ParsePlan(content, request)
}
