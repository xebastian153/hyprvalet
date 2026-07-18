// Package audio adapts wpctl (WirePlumber) into hyprvalet capabilities: volume
// and mute for the default output. Another edge of the hexagon.
package audio

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/xebastian153/hyprvalet/internal/core"
)

const defaultSink = "@DEFAULT_AUDIO_SINK@"

// Capabilities returns every audio capability.
func Capabilities() []core.Capability {
	return []core.Capability{setVolume{}, toggleMute{}}
}

func wpctl(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "wpctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("wpctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// percentArg validates the "percent" parameter: an integer from 0 to 100. The
// 100 cap is a business rule, not a tool limit — wpctl would happily boost
// beyond 100%, and a model asked to "turn it up" must not be able to blast the
// speakers past unity gain.
func percentArg(args core.Args) (int, error) {
	raw, ok := args["percent"]
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, core.Validationf("missing required arg %q (a volume from 0 to 100)", "percent")
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, core.Validationf("arg %q must be an integer from 0 to 100, got %q", "percent", raw)
	}
	if n < 0 || n > 100 {
		return 0, core.Validationf("arg %q must be between 0 and 100, got %d", "percent", n)
	}
	return n, nil
}

// setVolume sets the default output volume to an absolute percentage.
type setVolume struct{}

func (setVolume) ID() string              { return "volume.set" }
func (setVolume) Description() string     { return "Set the output volume to a percentage (0-100)" }
func (setVolume) Access() core.AccessKind { return core.AccessAudio }
func (setVolume) Risk() core.Risk         { return core.RiskSafe }
func (setVolume) Params() []string        { return []string{"percent"} }
func (setVolume) Run(ctx context.Context, args core.Args) (string, error) {
	n, err := percentArg(args)
	if err != nil {
		return "", err
	}
	if err := wpctl(ctx, "set-volume", defaultSink, fmt.Sprintf("%d%%", n)); err != nil {
		return "", err
	}
	return fmt.Sprintf("volume set to %d%%", n), nil
}

// toggleMute toggles mute on the default output.
type toggleMute struct{}

func (toggleMute) ID() string              { return "volume.mute" }
func (toggleMute) Description() string     { return "Toggle mute on the output audio" }
func (toggleMute) Access() core.AccessKind { return core.AccessAudio }
func (toggleMute) Risk() core.Risk         { return core.RiskSafe }
func (toggleMute) Params() []string        { return nil }
func (toggleMute) Run(ctx context.Context, _ core.Args) (string, error) {
	if err := wpctl(ctx, "set-mute", defaultSink, "toggle"); err != nil {
		return "", err
	}
	return "toggled mute", nil
}
