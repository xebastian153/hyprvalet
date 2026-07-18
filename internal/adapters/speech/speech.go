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
)

// Speaker turns text into audible speech, blocking until playback ends.
type Speaker interface {
	Speak(ctx context.Context, text string) error
}

// Chain tries each Speaker in order until one succeeds.
type Chain struct {
	speakers []Speaker
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
