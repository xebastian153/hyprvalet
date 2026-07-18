// Package omarchy adapts the Omarchy CLI into hyprvalet capabilities. Like the
// hypr adapter, it is an edge of the hexagon — the core depends only on the
// Capability interface, never on the omarchy binary.
package omarchy

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Capabilities returns every Omarchy-backed capability.
func Capabilities() []core.Capability {
	return []core.Capability{runCommand{}, openBrowser{}, openMusic{}}
}

// launch runs one of Omarchy's launcher scripts. They detach the launched app
// into its own scope (via uwsm), so the daemon never adopts a browser as a
// child for life.
func launch(ctx context.Context, bin string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, bin, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s: %w: %s", bin, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// openBrowser opens (or focuses) the user's default browser. The command is
// FIXED — no argument reaches a shell — which is what lets it be Safe-tier
// where the arbitrary app.open must be Confirm.
type openBrowser struct{}

func (openBrowser) ID() string              { return "browser.open" }
func (openBrowser) Description() string     { return "Open (or focus) the default web browser" }
func (openBrowser) Access() core.AccessKind { return core.AccessApp }
func (openBrowser) Risk() core.Risk         { return core.RiskSafe }
func (openBrowser) Params() []string        { return nil }
func (openBrowser) Run(ctx context.Context, _ core.Args) (string, error) {
	if out, err := launch(ctx, "omarchy-launch-browser"); err != nil {
		return out, err
	}
	return "opened the browser", nil
}

// openMusic opens (or focuses) the music player. Fixed command, Safe-tier for
// the same reason as openBrowser.
type openMusic struct{}

func (openMusic) ID() string              { return "music.open" }
func (openMusic) Description() string     { return "Open (or focus) the music player (Spotify)" }
func (openMusic) Access() core.AccessKind { return core.AccessApp }
func (openMusic) Risk() core.Risk         { return core.RiskSafe }
func (openMusic) Params() []string        { return nil }
func (openMusic) Run(ctx context.Context, _ core.Args) (string, error) {
	if out, err := launch(ctx, "omarchy-launch-or-focus", "spotify"); err != nil {
		return out, err
	}
	return "opened the music player", nil
}

type runCommand struct{}

func (runCommand) ID() string { return "omarchy.run" }
func (runCommand) Description() string {
	return "Run an omarchy CLI subcommand (e.g. \"restart waybar\")"
}
func (runCommand) Access() core.AccessKind { return core.AccessCommand }

// Risk is Confirm: omarchy subcommands can restart services or refresh configs,
// which is disruptive and not always trivially reversible.
func (runCommand) Risk() core.Risk { return core.RiskConfirm }

func (runCommand) Params() []string { return []string{"args"} }

func (runCommand) Run(ctx context.Context, args core.Args) (string, error) {
	raw := strings.TrimSpace(args["args"])
	if raw == "" {
		return "", core.Validationf("missing required arg %q (the omarchy subcommand, e.g. \"restart waybar\")", "args")
	}
	parts := strings.Fields(raw)
	out, err := exec.CommandContext(ctx, "omarchy", parts...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("omarchy %s: %w: %s", raw, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
