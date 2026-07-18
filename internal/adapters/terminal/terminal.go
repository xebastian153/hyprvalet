// Package terminal gives the assistant eyes into the Claude Code sessions it
// opened: it reads back what tmux shows. Because Claude runs inside a named
// tmux session (see the project adapter), its screen can be captured with
// `tmux capture-pane` — no screen scraping, no OCR.
package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/xebastian153/hyprvalet/internal/adapters/project"
	"github.com/xebastian153/hyprvalet/internal/core"
)

// Capabilities returns every terminal capability.
func Capabilities() []core.Capability {
	return []core.Capability{readTerminal{}, sendTerminal{}}
}

// readLines is how many meaningful (non-decoration) lines of the terminal tail
// the read returns — enough to convey what Claude is showing without reciting
// a whole screen.
const readLines = 14

// readTerminal reports what the Claude Code terminal is currently showing.
type readTerminal struct{}

func (readTerminal) ID() string { return "terminal.read" }
func (readTerminal) Description() string {
	return "Read what the Claude Code terminal is currently showing"
}
func (readTerminal) Access() core.AccessKind { return core.AccessCommand }
func (readTerminal) Risk() core.Risk         { return core.RiskSafe }
func (readTerminal) Params() []string        { return nil }
func (readTerminal) Run(ctx context.Context, _ core.Args) (string, error) {
	text, err := Capture(ctx, readLines)
	if err != nil {
		return "", err
	}
	if text == "" {
		return "the terminal is empty", nil
	}
	return text, nil
}

// Capture returns the last n meaningful lines of the most-recently-active
// Claude terminal, cleaned of TUI decoration. It is the shared read used both
// by the terminal.read capability and by the reasoning layer, which folds this
// into the prompt so the assistant can explain what Claude is doing rather than
// merely recite it. A corrective error means there is nothing to read.
func Capture(ctx context.Context, n int) (string, error) {
	session, err := activeSession(ctx)
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", session).Output()
	if err != nil {
		return "", fmt.Errorf("reading the terminal: %w", err)
	}
	return cleanCapture(string(out), n), nil
}

// sendTerminal types a line into the Claude Code terminal — the assistant
// relaying your words to Claude. This is the most powerful capability: whatever
// is typed goes to whatever occupies that pane (usually Claude, which has its
// own confirmations; a bare shell would run it). So it is Confirm-tier — you
// approve each message before it is sent — and the assistant relays, it never
// answers Claude on its own. The message is passed to tmux as a literal
// argument, never through a shell.
type sendTerminal struct{}

func (sendTerminal) ID() string { return "terminal.send" }
func (sendTerminal) Description() string {
	return "Type a message into the Claude Code terminal (relay your words to Claude)"
}
func (sendTerminal) Access() core.AccessKind { return core.AccessCommand }
func (sendTerminal) Risk() core.Risk         { return core.RiskConfirm }
func (sendTerminal) Params() []string        { return []string{"text"} }
func (sendTerminal) Run(ctx context.Context, args core.Args) (string, error) {
	text := strings.TrimSpace(args["text"])
	if text == "" {
		return "", core.Validationf("missing required arg %q (what to type into the terminal)", "text")
	}
	session, err := activeSession(ctx)
	if err != nil {
		return "", err
	}
	// Send the text literally (-l, so no character is read as a key name),
	// then a separate Enter to submit it.
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", session, "-l", "--", text).Run(); err != nil {
		return "", fmt.Errorf("typing into the terminal: %w", err)
	}
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", session, "Enter").Run(); err != nil {
		return "", fmt.Errorf("submitting to the terminal: %w", err)
	}
	return fmt.Sprintf("sent to Claude: %q", text), nil
}

// activeSession finds the most-recently-active hyprvalet tmux session — the
// Claude terminal the user most likely means. A missing tmux server or no such
// session is a corrective "nothing to read", not a crash.
func activeSession(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "tmux", "list-sessions",
		"-F", "#{session_activity} #{session_name}").Output()
	if err != nil {
		return "", core.Validationf("no Claude terminal is open — open a project first")
	}
	type sess struct {
		activity string
		name     string
	}
	var sessions []sess
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		activity, name, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && strings.HasPrefix(name, project.SessionPrefix) {
			sessions = append(sessions, sess{activity, name})
		}
	}
	if len(sessions) == 0 {
		return "", core.Validationf("no Claude terminal is open — open a project first")
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].activity > sessions[j].activity })
	return sessions[0].name, nil
}

// cleanCapture turns a raw tmux pane dump into readable text: it drops trailing
// blank space, skips lines that are only box-drawing or punctuation (a TUI's
// borders carry no meaning aloud), and keeps the last n meaningful lines.
func cleanCapture(raw string, n int) string {
	var kept []string
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		if !hasMeaning(trimmed) {
			continue // pure border/decoration
		}
		kept = append(kept, strings.TrimSpace(trimmed))
	}
	if len(kept) > n {
		kept = kept[len(kept)-n:]
	}
	return strings.Join(kept, "\n")
}

// hasMeaning reports whether a line carries readable content — at least one
// letter or digit — rather than being only box-drawing and punctuation.
func hasMeaning(line string) bool {
	for _, r := range line {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return true
		}
	}
	return false
}
