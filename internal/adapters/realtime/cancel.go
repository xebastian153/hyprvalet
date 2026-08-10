package realtime

import (
	"context"
	"sync"
)

// CancelScope mirrors the speech-to-speech sidecar generation for barge-in.
// Every speech_started with interrupt=true increments generation; stale events
// with Generation < current are discarded. Thread-safe via mutex.
//
// Matches sidecar `_generation_is_discardable` semantics: gen monotonically
// increases, events stamped with older gen are ignored.
type CancelScope struct {
	mu     sync.Mutex
	gen    int
	ctx    context.Context
	cancel context.CancelFunc
}

// ServerEvent is the minimal wire event for queue handling. Real sidecar
// events include transcript.delta, output_audio.delta, response.done, etc.
// Only SESSION_END is preserved across FlushQueues.
type ServerEvent struct {
	Type       string `json:"type"`
	Generation int    `json:"generation,omitempty"`
	Payload    []byte `json:"payload,omitempty"`
}

// NewCancelScope creates a scope at generation 0 with a cancellable context.
func NewCancelScope() *CancelScope {
	ctx, cancel := context.WithCancel(context.Background())
	return &CancelScope{ctx: ctx, cancel: cancel}
}

// Gen returns the current generation (thread-safe).
func (c *CancelScope) Gen() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

// Generation is an alias for Gen for compatibility with design doc naming.
func (c *CancelScope) Generation() int { return c.Gen() }

// Cancel increments generation, cancels the previous context, creates a new
// one, and returns the new generation. Thread-safe.
func (c *CancelScope) Cancel() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	if c.cancel != nil {
		c.cancel()
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	return c.gen
}

// IsStale reports whether gen is stale (gen < current). Future generations
// are not considered stale — they belong to a newer turn not yet observed.
func (c *CancelScope) IsStale(gen int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return gen < c.gen
}

// Context returns the context bound to the current generation. Cancel() cancels
// the previous context; the returned context is cancelled on next Cancel().
func (c *CancelScope) Context() context.Context {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ctx
}

// FlushQueues returns only events where Type == "SESSION_END", discarding all
// other queued deltas. Preserves SESSION_END per spec (4 queues flushed but
// session terminator survives). Thread-safe but does not mutate scope state.
func (c *CancelScope) FlushQueues(events []ServerEvent) []ServerEvent {
	out := make([]ServerEvent, 0, len(events))
	for _, ev := range events {
		if ev.Type == "SESSION_END" {
			out = append(out, ev)
		}
	}
	return out
}

// FlushQueues is a package-level helper that preserves only SESSION_END.
// Provided for callers without a CancelScope instance.
func FlushQueues(events []ServerEvent) []ServerEvent {
	out := make([]ServerEvent, 0, len(events))
	for _, ev := range events {
		if ev.Type == "SESSION_END" {
			out = append(out, ev)
		}
	}
	return out
}
