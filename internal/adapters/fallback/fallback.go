// Package fallback composes two reasoning providers into one resilient port:
// try the primary (cloud — fast, smart), and when it fails — no network, bad
// key, provider down — silently use the backup (local — always there). The
// agent degrades in quality, never in availability.
package fallback

import (
	"context"
	"fmt"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Reasoner is both reasoning ports in one — what every LLM adapter implements.
type Reasoner interface {
	core.LLMPort
	core.PlannerPort
}

// Client tries primary and falls back to backup. It implements Reasoner.
type Client struct {
	primary Reasoner
	backup  Reasoner
}

// New composes primary-with-backup.
func New(primary, backup Reasoner) *Client {
	return &Client{primary: primary, backup: backup}
}

// Interpret satisfies core.LLMPort.
func (c *Client) Interpret(ctx context.Context, request string, caps []core.Capability, recent []core.Event) (core.Intent, error) {
	intent, err := c.primary.Interpret(ctx, request, caps, recent)
	if err == nil {
		return intent, nil
	}
	intent, berr := c.backup.Interpret(ctx, request, caps, recent)
	if berr != nil {
		return core.Intent{}, fmt.Errorf("primary failed (%v); backup failed too: %w", err, berr)
	}
	return intent, nil
}

// Plan satisfies core.PlannerPort.
func (c *Client) Plan(ctx context.Context, request string, caps []core.Capability, recent []core.Event) (core.Plan, error) {
	plan, err := c.primary.Plan(ctx, request, caps, recent)
	if err == nil {
		return plan, nil
	}
	plan, berr := c.backup.Plan(ctx, request, caps, recent)
	if berr != nil {
		return core.Plan{}, fmt.Errorf("primary failed (%v); backup failed too: %w", err, berr)
	}
	return plan, nil
}
