// Package hypr adapts Hyprland (via the hyprctl IPC) into hyprvalet
// capabilities. It is one edge of the hexagon: the core knows nothing about
// hyprctl, only about the Capability interface these types implement.
package hypr

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/SebasDevMag/hyprvalet/internal/core"
)

// Capabilities returns every Hyprland-backed capability.
func Capabilities() []core.Capability {
	return []core.Capability{
		switchWorkspace{},
		moveWindowToWorkspace{},
		openApp{},
	}
}

// dispatch runs `hyprctl dispatch <args...>` and returns trimmed output.
func dispatch(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"dispatch"}, args...)
	out, err := exec.CommandContext(ctx, "hyprctl", full...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("hyprctl %s: %w: %s",
			strings.Join(full, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// shellMeta are characters a shell would interpret to chain, redirect, expand,
// or substitute commands. hyprctl runs an exec string through a shell, so any
// of these could smuggle a second command past a single app launch.
const shellMeta = ";&|<>$`\n\r\\!*?(){}[]"

// validateLaunchCmd trims and checks the "cmd" arg for app.open. It guarantees a
// single, metacharacter-free launch command so a caller cannot chain a second
// command onto a launch. It deliberately does NOT vouch for the binary itself —
// "rm -rf ~" is metacharacter-free yet destructive — which is why app.open is
// Confirm-tier rather than Safe.
func validateLaunchCmd(raw string) (string, error) {
	cmd := strings.TrimSpace(raw)
	if cmd == "" {
		return "", fmt.Errorf("missing required arg %q (the command to launch)", "cmd")
	}
	if i := strings.IndexAny(cmd, shellMeta); i >= 0 {
		return "", fmt.Errorf(
			"arg %q may not contain the shell metacharacter %q — app.open launches a single program (e.g. %q or %q), not a shell command",
			"cmd", string(cmd[i]), "firefox", "alacritty -e nvim")
	}
	return cmd, nil
}

// workspaceArg validates and parses the shared "workspace" parameter.
func workspaceArg(args core.Args) (int, error) {
	raw, ok := args["workspace"]
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, fmt.Errorf("missing required arg %q (a workspace number)", "workspace")
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, fmt.Errorf("arg %q must be an integer, got %q", "workspace", raw)
	}
	if n < 1 {
		return 0, fmt.Errorf("arg %q must be >= 1, got %d", "workspace", n)
	}
	return n, nil
}

type switchWorkspace struct{}

func (switchWorkspace) ID() string              { return "workspace.switch" }
func (switchWorkspace) Description() string     { return "Switch to a workspace by number" }
func (switchWorkspace) Access() core.AccessKind { return core.AccessWorkspace }
func (switchWorkspace) Risk() core.Risk         { return core.RiskSafe }
func (switchWorkspace) Params() []string        { return []string{"workspace"} }
func (switchWorkspace) Run(ctx context.Context, args core.Args) (string, error) {
	n, err := workspaceArg(args)
	if err != nil {
		return "", err
	}
	if _, err := dispatch(ctx, "workspace", strconv.Itoa(n)); err != nil {
		return "", err
	}
	return fmt.Sprintf("switched to workspace %d", n), nil
}

type moveWindowToWorkspace struct{}

func (moveWindowToWorkspace) ID() string { return "window.move_to_workspace" }
func (moveWindowToWorkspace) Description() string {
	return "Move the active window to a workspace by number"
}
func (moveWindowToWorkspace) Access() core.AccessKind { return core.AccessWindow }
func (moveWindowToWorkspace) Risk() core.Risk         { return core.RiskSafe }
func (moveWindowToWorkspace) Params() []string        { return []string{"workspace"} }
func (moveWindowToWorkspace) Run(ctx context.Context, args core.Args) (string, error) {
	n, err := workspaceArg(args)
	if err != nil {
		return "", err
	}
	if _, err := dispatch(ctx, "movetoworkspace", strconv.Itoa(n)); err != nil {
		return "", err
	}
	return fmt.Sprintf("moved active window to workspace %d", n), nil
}

type openApp struct{}

func (openApp) ID() string              { return "app.open" }
func (openApp) Description() string     { return "Launch an application via hyprctl exec" }
func (openApp) Access() core.AccessKind { return core.AccessApp }

// Risk is Confirm, not Safe. hyprctl runs the exec string through a shell.
// validateLaunchCmd stops command *chaining*, but it cannot tell a benign
// launcher ("firefox") from a destructive binary ("rm -rf ~") — both are single,
// metacharacter-free commands. Until an app allowlist lands (M1), the only
// remaining safety property is human-in-the-loop, so every launch is confirmed.
func (openApp) Risk() core.Risk  { return core.RiskConfirm }
func (openApp) Params() []string { return []string{"cmd"} }
func (openApp) Run(ctx context.Context, args core.Args) (string, error) {
	cmd, err := validateLaunchCmd(args["cmd"])
	if err != nil {
		return "", err
	}
	if _, err := dispatch(ctx, "exec", cmd); err != nil {
		return "", err
	}
	return fmt.Sprintf("launched %q", cmd), nil
}
