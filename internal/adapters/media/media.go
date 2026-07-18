// Package media adapts playerctl (MPRIS) into hyprvalet capabilities: playback
// control for whatever player is active. Another edge of the hexagon — the core
// knows only the Capability interface.
package media

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Capabilities returns every playback capability.
func Capabilities() []core.Capability {
	return []core.Capability{
		control{id: "media.play_pause", desc: "Toggle music/video playback (play or pause)", verb: "play-pause", done: "toggled playback"},
		control{id: "media.next", desc: "Skip to the next track", verb: "next", done: "skipped to the next track"},
		control{id: "media.previous", desc: "Go back to the previous track", verb: "previous", done: "went to the previous track"},
	}
}

// control is one fixed playerctl verb. No parameters, nothing to validate,
// nothing a model can inject — which is why all three are Safe-tier.
type control struct {
	id, desc, verb, done string
}

func (c control) ID() string            { return c.id }
func (c control) Description() string   { return c.desc }
func (control) Access() core.AccessKind { return core.AccessMedia }
func (control) Risk() core.Risk         { return core.RiskSafe }
func (control) Params() []string        { return nil }
func (c control) Run(ctx context.Context, _ core.Args) (string, error) {
	out, err := exec.CommandContext(ctx, "playerctl", c.verb).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("playerctl %s: %w: %s", c.verb, err, strings.TrimSpace(string(out)))
	}
	return c.done, nil
}
