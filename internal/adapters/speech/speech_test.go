package speech

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type canned struct {
	err   error
	spoke *bool
}

func (c canned) Speak(context.Context, string) error {
	if c.err == nil && c.spoke != nil {
		*c.spoke = true
	}
	return c.err
}

func TestChainFirstSuccessWins(t *testing.T) {
	first, second := false, false
	c := NewChain(canned{spoke: &first}, canned{spoke: &second})
	if err := c.Speak(context.Background(), "hi"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if !first || second {
		t.Fatalf("first=%v second=%v — only the first healthy backend must speak", first, second)
	}
}

func TestChainFallsThrough(t *testing.T) {
	spoke := false
	c := NewChain(canned{err: errors.New("no credits")}, canned{spoke: &spoke})
	if err := c.Speak(context.Background(), "hi"); err != nil || !spoke {
		t.Fatalf("err=%v spoke=%v — the backup must speak when the primary fails", err, spoke)
	}
}

func TestChainAllFailingReportsAll(t *testing.T) {
	c := NewChain(canned{err: errors.New("no credits")}, canned{err: errors.New("no network")})
	err := c.Speak(context.Background(), "hi")
	if err == nil || !strings.Contains(err.Error(), "no credits") || !strings.Contains(err.Error(), "no network") {
		t.Fatalf("err = %v, want both causes", err)
	}
}

func TestChainStopsOnCancelNoReplay(t *testing.T) {
	// A cancelled context is an interruption, not a failure: the chain must
	// NOT fall through and replay the words on the next backend.
	spokeSecond := false
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := NewChain(
		canned{err: context.Canceled},
		canned{spoke: &spokeSecond},
	)
	if err := c.Speak(ctx, "hi"); err == nil {
		t.Fatal("cancelled chain must return an error, not success")
	}
	if spokeSecond {
		t.Fatal("a barge-in must not replay the words on the fallback backend")
	}
}
