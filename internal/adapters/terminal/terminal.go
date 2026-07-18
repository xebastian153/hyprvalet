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
	return []core.Capability{readTerminal{}}
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
	session, err := activeSession(ctx)
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", session).Output()
	if err != nil {
		return "", fmt.Errorf("reading the terminal: %w", err)
	}
	text := cleanCapture(string(out), readLines)
	if text == "" {
		return "the terminal is empty", nil
	}
	return text, nil
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
