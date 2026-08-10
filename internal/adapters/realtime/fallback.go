package realtime

import (
	"context"
	"fmt"
	"os"

	"github.com/xebastian153/hyprvalet/internal/adapters/fallback"
	"github.com/xebastian153/hyprvalet/internal/core"
)

// Fallback composes a realtime primary with a batch backup, handling
// WS down, offline, and quarantine without crashing. When the realtime
// sidecar is quarantined (180s) or unreachable, it degrades to batch
// and emits a visible note. Mirrors fallback.Client but with
// realtime-specific quarantine gate and richer degrade logging.
type Fallback struct {
	primary     fallback.Reasoner
	backup      fallback.Reasoner
	quarantined func() bool
}

// NewRealtimeFallback creates a realtime→batch fallback.
// isQuarantined may be nil (no quarantine check); when it returns true the
// primary is skipped entirely and backup is used.
func NewRealtimeFallback(primary, backup fallback.Reasoner, isQuarantined func() bool) *Fallback {
	return &Fallback{primary: primary, backup: backup, quarantined: isQuarantined}
}

// Interpret tries primary unless quarantined, then backup. Degradation is noted
// to stderr so the journal and interactive shells see it without threading a
// logger through the port.
func (f *Fallback) Interpret(ctx context.Context, request string, caps []core.Capability, recent []core.Event) (core.Intent, error) {
	if f.quarantined != nil && f.quarantined() {
		noteRealtime("quarantined (180s) — streaming unavailable")
		return f.backup.Interpret(ctx, request, caps, recent)
	}
	intent, err := f.primary.Interpret(ctx, request, caps, recent)
	if err == nil {
		return intent, nil
	}
	noteRealtimeErr(err)
	intent, berr := f.backup.Interpret(ctx, request, caps, recent)
	if berr != nil {
		return core.Intent{}, fmt.Errorf("realtime primary failed (%v); batch backup failed too: %w", err, berr)
	}
	return intent, nil
}

// Plan mirrors Interpret for multi-step planning.
func (f *Fallback) Plan(ctx context.Context, request string, caps []core.Capability, recent []core.Event) (core.Plan, error) {
	if f.quarantined != nil && f.quarantined() {
		noteRealtime("quarantined (180s) — planning via batch")
		return f.backup.Plan(ctx, request, caps, recent)
	}
	plan, err := f.primary.Plan(ctx, request, caps, recent)
	if err == nil {
		return plan, nil
	}
	noteRealtimeErr(err)
	plan, berr := f.backup.Plan(ctx, request, caps, recent)
	if berr != nil {
		return core.Plan{}, fmt.Errorf("realtime primary failed (%v); batch backup failed too: %w", err, berr)
	}
	return plan, nil
}

// Chat tries primary chat unless quarantined, then backup.
func (f *Fallback) Chat(ctx context.Context, system, user string) (string, error) {
	if f.quarantined != nil && f.quarantined() {
		noteRealtime("quarantined — chat via batch")
		if c, ok := f.backup.(interface {
			Chat(context.Context, string, string) (string, error)
		}); ok {
			return c.Chat(ctx, system, user)
		}
		return "", fmt.Errorf("no chat-capable backup available (quarantined)")
	}
	// Try primary if it implements Chat
	if p, ok := f.primary.(interface {
		Chat(context.Context, string, string) (string, error)
	}); ok {
		if out, err := p.Chat(ctx, system, user); err == nil {
			return out, nil
		} else {
			noteRealtimeErr(err)
		}
	}
	if b, ok := f.backup.(interface {
		Chat(context.Context, string, string) (string, error)
	}); ok {
		return b.Chat(ctx, system, user)
	}
	return "", fmt.Errorf("no chat-capable reasoning backend available")
}

func noteRealtime(msg string) {
	fmt.Fprintf(os.Stderr, "realtime: %s — falling back to batch (local Ollama http://localhost:11434/v1, quality degraded but available)\n", msg)
}

func noteRealtimeErr(err error) {
	fmt.Fprintf(os.Stderr, "realtime: primary failed (%v) — falling back to batch (local Ollama http://localhost:11434/v1)\n", err)
}
