// Package speech defines the speaking boundary of the voice frontend: a
// Speaker turns text into audible words, and a Chain composes several backends
// by quality — cloud-natural first, local-robotic last — so speech degrades in
// beauty, never in availability. The same resilience shape as the reasoning
// fallback, applied to the mouth instead of the brain.
package speech

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Speaker turns text into audible speech, blocking until playback ends.
type Speaker interface {
	Speak(ctx context.Context, text string) error
}

// Chain tries each Speaker in order until one succeeds.
type Chain struct {
	speakers    []Speaker
	lastBackend string
}

// NewChain composes speakers by preference order.
func NewChain(speakers ...Speaker) *Chain {
	return &Chain{speakers: speakers}
}

// Speak satisfies Speaker: the first backend that speaks wins; when all fail,
// every cause is reported. A cancelled context stops the chain immediately —
// it is an interruption (barge-in), not a backend failure, so the next backend
// must NOT replay the same words.
func (c *Chain) Speak(ctx context.Context, text string) error {
	var errs []error
	for _, s := range c.speakers {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		err := s.Speak(ctx, text)
		if err == nil {
			c.lastBackend = speakerName(s)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return fmt.Errorf("no speech backends configured")
	}
	return errors.Join(errs...)
}

// LastBackend returns the name of the backend that last succeeded (e.g.
// "elevenlabs", "edge-tts", "piper"), or "" if none has succeeded yet.
func (c *Chain) LastBackend() string { return c.lastBackend }

// speakerName resolves a backend name for timing labels. Known speakers
// implement Name(); otherwise the type is mapped to a friendly label so the
// timing line shows "piper" / "edge-tts" / "elevenlabs" instead of a Go type.
// This keeps concrete adapters free to opt into Name() without requiring the
// speech package to import them.
func speakerName(s Speaker) string {
	if n, ok := s.(interface{ Name() string }); ok {
		if name := n.Name(); name != "" {
			return name
		}
	}
	t := fmt.Sprintf("%T", s)
	low := strings.ToLower(t)
	switch {
	case strings.Contains(low, "elevenlabs"):
		return "elevenlabs"
	case strings.Contains(low, "edgetts"):
		return "edge-tts"
	case strings.Contains(low, "tts"):
		// Covers *tts.Client (piper) but not edgetts (already handled).
		return "piper"
	default:
		if idx := strings.LastIndex(t, "."); idx >= 0 && idx+1 < len(t) {
			return strings.ToLower(t[idx+1:])
		}
		return low
	}
}
