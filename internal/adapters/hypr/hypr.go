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

func (switchWorkspace) ID() string             { return "workspace.switch" }
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

func (moveWindowToWorkspace) ID() string             { return "window.move_to_workspace" }
func (moveWindowToWorkspace) Description() string     { return "Move the active window to a workspace by number" }
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

func (openApp) ID() string             { return "app.open" }
func (openApp) Description() string     { return "Launch an application via hyprctl exec" }
func (openApp) Access() core.AccessKind { return core.AccessApp }
func (openApp) Risk() core.Risk         { return core.RiskSafe }
func (openApp) Params() []string        { return []string{"cmd"} }
func (openApp) Run(ctx context.Context, args core.Args) (string, error) {
	cmd := strings.TrimSpace(args["cmd"])
	if cmd == "" {
		return "", fmt.Errorf("missing required arg %q (the command to launch)", "cmd")
	}
	if _, err := dispatch(ctx, "exec", cmd); err != nil {
		return "", err
	}
	return fmt.Sprintf("launched %q", cmd), nil
}
