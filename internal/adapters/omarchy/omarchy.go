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
	return []core.Capability{runCommand{}}
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
		return "", fmt.Errorf("missing required arg %q (the omarchy subcommand, e.g. \"restart waybar\")", "args")
	}
	parts := strings.Fields(raw)
	out, err := exec.CommandContext(ctx, "omarchy", parts...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("omarchy %s: %w: %s", raw, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
