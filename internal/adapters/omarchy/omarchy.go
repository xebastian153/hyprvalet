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
	return []core.Capability{
		runCommand{}, openBrowser{}, openMusic{},
		takeScreenshot{}, themeNext{}, themeSet{}, nightlightToggle{}, lockScreen{},
	}
}

// omarchyCmd runs one omarchy CLI subcommand with fixed, typed arguments.
func omarchyCmd(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "omarchy", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("omarchy %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
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

// takeScreenshot captures the full screen non-interactively — a voice command
// cannot drag a region selector.
type takeScreenshot struct{}

func (takeScreenshot) ID() string              { return "screenshot.take" }
func (takeScreenshot) Description() string     { return "Take a fullscreen screenshot" }
func (takeScreenshot) Access() core.AccessKind { return core.AccessCommand }
func (takeScreenshot) Risk() core.Risk         { return core.RiskSafe }
func (takeScreenshot) Params() []string        { return nil }
func (takeScreenshot) Run(ctx context.Context, _ core.Args) (string, error) {
	if _, err := omarchyCmd(ctx, "capture", "screenshot", "fullscreen"); err != nil {
		return "", err
	}
	return "took a screenshot", nil
}

// themeNext cycles to the next installed theme. Omarchy has no `theme next`
// subcommand, so the ring is ours: current → list → the one after → set.
type themeNext struct{}

func (themeNext) ID() string              { return "theme.next" }
func (themeNext) Description() string     { return "Switch to the next desktop theme" }
func (themeNext) Access() core.AccessKind { return core.AccessCommand }
func (themeNext) Risk() core.Risk         { return core.RiskSafe }
func (themeNext) Params() []string        { return nil }
func (themeNext) Run(ctx context.Context, _ core.Args) (string, error) {
	current, err := omarchyCmd(ctx, "theme", "current")
	if err != nil {
		return "", err
	}
	list, err := omarchyCmd(ctx, "theme", "list")
	if err != nil {
		return "", err
	}
	next := nextTheme(current, strings.Split(list, "\n"))
	if next == "" {
		return "", fmt.Errorf("no themes installed")
	}
	if _, err := omarchyCmd(ctx, "theme", "set", next); err != nil {
		return "", err
	}
	return fmt.Sprintf("switched to theme %s", next), nil
}

// nextTheme picks the entry after current in the installed ring, wrapping at
// the end; an unknown current lands on the first theme.
func nextTheme(current string, installed []string) string {
	var names []string
	for _, line := range installed {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	current = strings.TrimSpace(current)
	for i, name := range names {
		if name == current {
			return names[(i+1)%len(names)]
		}
	}
	return names[0]
}

// themeSet applies a named theme. The name is validated against the LIVE list
// of installed themes — a dynamic allowlist. A hallucinated theme dies here
// with a corrective error that carries the real options, which the retry loop
// feeds back so the model can pick one that exists.
type themeSet struct{}

func (themeSet) ID() string              { return "theme.set" }
func (themeSet) Description() string     { return "Apply a desktop theme by name" }
func (themeSet) Access() core.AccessKind { return core.AccessCommand }
func (themeSet) Risk() core.Risk         { return core.RiskSafe }
func (themeSet) Params() []string        { return []string{"name"} }
func (themeSet) Run(ctx context.Context, args core.Args) (string, error) {
	want := strings.TrimSpace(args["name"])
	if want == "" {
		return "", core.Validationf("missing required arg %q (a theme name)", "name")
	}
	list, err := omarchyCmd(ctx, "theme", "list")
	if err != nil {
		return "", err
	}
	name, err := matchTheme(want, strings.Split(list, "\n"))
	if err != nil {
		return "", err
	}
	if _, err := omarchyCmd(ctx, "theme", "set", name); err != nil {
		return "", err
	}
	return fmt.Sprintf("applied theme %s", name), nil
}

// matchTheme resolves a requested theme against the installed list,
// case-insensitively and tolerating space/underscore/hyphen differences.
func matchTheme(want string, installed []string) (string, error) {
	canon := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		s = strings.NewReplacer(" ", "", "_", "", "-", "").Replace(s)
		return s
	}
	var names []string
	for _, line := range installed {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		names = append(names, name)
		if canon(name) == canon(want) {
			return name, nil
		}
	}
	return "", core.Validationf("unknown theme %q — installed themes are: %s", want, strings.Join(names, ", "))
}

// nightlightToggle toggles the blue-light filter.
type nightlightToggle struct{}

func (nightlightToggle) ID() string              { return "nightlight.toggle" }
func (nightlightToggle) Description() string     { return "Toggle the night light (blue-light filter)" }
func (nightlightToggle) Access() core.AccessKind { return core.AccessCommand }
func (nightlightToggle) Risk() core.Risk         { return core.RiskSafe }
func (nightlightToggle) Params() []string        { return nil }
func (nightlightToggle) Run(ctx context.Context, _ core.Args) (string, error) {
	if _, err := omarchyCmd(ctx, "toggle", "nightlight"); err != nil {
		return "", err
	}
	return "toggled the night light", nil
}

// lockScreen locks the session. Risk is Confirm, not Safe: it is reversible
// (with the password) but disruptive — a misheard command must not kick the
// user out of their session unannounced, as a garbled "what time is it" once
// did. Locking rarely needs to be instant, so it earns a confirmation.
type lockScreen struct{}

func (lockScreen) ID() string              { return "system.lock" }
func (lockScreen) Description() string     { return "Lock the screen" }
func (lockScreen) Access() core.AccessKind { return core.AccessCommand }
func (lockScreen) Risk() core.Risk         { return core.RiskConfirm }
func (lockScreen) Params() []string        { return nil }
func (lockScreen) Run(ctx context.Context, _ core.Args) (string, error) {
	if _, err := omarchyCmd(ctx, "system", "lock"); err != nil {
		return "", err
	}
	return "locked the screen", nil
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
