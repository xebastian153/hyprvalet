// Package fallback composes two reasoning providers into one resilient port:
// try the primary (cloud — fast, smart), and when it fails — no network, bad
// key, provider down — silently use the backup (local — always there). The
// agent degrades in quality, never in availability.
package fallback

import (
	"context"
	"fmt"
	"os"

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
	note(err)
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
	note(err)
	plan, berr := c.backup.Plan(ctx, request, caps, recent)
	if berr != nil {
		return core.Plan{}, fmt.Errorf("primary failed (%v); backup failed too: %w", err, berr)
	}
	return plan, nil
}

// chatter is a raw conversational turn — a capability some reasoners have
// beyond the typed ports. Detected by assertion so the Reasoner interface (and
// its test stubs) stay untouched.
type chatter interface {
	Chat(ctx context.Context, system, user string) (string, error)
}

// Chat tries the primary reasoner and falls back to the backup, mirroring the
// resilience of Interpret/Plan. A backend that cannot chat is skipped.
func (c *Client) Chat(ctx context.Context, system, user string) (string, error) {
	if p, ok := c.primary.(chatter); ok {
		if out, err := p.Chat(ctx, system, user); err == nil {
			return out, nil
		} else {
			note(err)
		}
	}
	if b, ok := c.backup.(chatter); ok {
		return b.Chat(ctx, system, user)
	}
	return "", fmt.Errorf("no chat-capable reasoning backend available")
}

// note makes degradation VISIBLE: a silent fallback once turned a knowledge
// question into a screen lock, and nobody could tell which model had answered.
// Written to stderr so the daemon's journal and an interactive shell both see
// it without threading a logger through the port.
func note(err error) {
	fmt.Fprintf(os.Stderr, "reasoning: primary provider failed (%v) — answering with the local fallback\n", err)
}
