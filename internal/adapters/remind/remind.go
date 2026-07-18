// Package remind gives the assistant proactivity: a capability that schedules
// its own voice to speak later. "Remind me about the coffee in 15 minutes"
// becomes a transient systemd user timer that runs `hyprvalet say <message>` —
// the assistant addresses the user first, which is what proactive means.
//
// No shell is involved: the scheduled command is an argument vector, so a
// message can never smuggle a second command.
package remind

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Capabilities returns every reminder capability.
func Capabilities() []core.Capability {
	return []core.Capability{setReminder{}}
}

// forwardedEnv are the settings the spoken reminder needs at fire time —
// systemd user units do not inherit the scheduler's environment, so the voice
// chain's configuration travels explicitly.
var forwardedEnv = []string{
	"PATH", "ELEVENLABS_API_KEY", "HYPRVALET_ELEVENLABS_VOICE", "HYPRVALET_ELEVENLABS_MODEL",
	"HYPRVALET_EDGE_VOICE", "HYPRVALET_VOICE", "HYPRVALET_LANG",
}

// setReminder schedules a spoken reminder N minutes from now.
type setReminder struct{}

func (setReminder) ID() string { return "reminder.set" }
func (setReminder) Description() string {
	return "Set a spoken reminder: say a message to the user after N minutes"
}
func (setReminder) Access() core.AccessKind { return core.AccessCommand }
func (setReminder) Risk() core.Risk         { return core.RiskSafe }
func (setReminder) Params() []string        { return []string{"minutes", "message"} }
func (setReminder) Run(ctx context.Context, args core.Args) (string, error) {
	minutes, err := minutesArg(args)
	if err != nil {
		return "", err
	}
	message := strings.TrimSpace(args["message"])
	if message == "" {
		return "", core.Validationf("missing required arg %q (what to say when the reminder fires)", "message")
	}
	if len(message) > 200 {
		return "", core.Validationf("arg %q is too long (%d chars, max 200)", "message", len(message))
	}

	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("locating hyprvalet binary: %w", err)
	}

	cmd := []string{
		"--user", "--collect", "--quiet",
		fmt.Sprintf("--on-active=%dm", minutes),
		"--timer-property=AccuracySec=1s",
	}
	for _, key := range forwardedEnv {
		if v := os.Getenv(key); v != "" {
			cmd = append(cmd, "--setenv="+key+"="+v)
		}
	}
	cmd = append(cmd, "--", self, "say", message)

	out, err := exec.CommandContext(ctx, "systemd-run", cmd...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("scheduling reminder: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return fmt.Sprintf("reminder set — I will speak up in %d minutes", minutes), nil
}

// minutesArg validates the "minutes" parameter: an integer from 1 to 1440 (a
// day). The cap is a business rule — a reminder further out than a day is a
// calendar's job, not a transient timer's.
func minutesArg(args core.Args) (int, error) {
	raw, ok := args["minutes"]
	if !ok || strings.TrimSpace(raw) == "" {
		return 0, core.Validationf("missing required arg %q (how many minutes from now)", "minutes")
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0, core.Validationf("arg %q must be an integer number of minutes, got %q", "minutes", raw)
	}
	if n < 1 || n > 1440 {
		return 0, core.Validationf("arg %q must be between 1 and 1440 minutes, got %d", "minutes", n)
	}
	return n, nil
}
