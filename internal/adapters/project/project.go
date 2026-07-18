// Package project scaffolds and opens coding projects: create a folder under a
// fixed projects directory and open Claude Code in it. The project name is
// slugified to a single safe path segment, so a spoken name can never escape
// the projects directory or reach a shell.
package project

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Capabilities returns every project capability.
func Capabilities() []core.Capability {
	return []core.Capability{newProject{}, openProject{}}
}

// baseDir is where projects live: $HYPRVALET_PROJECTS_DIR, else ~/proyectos.
func baseDir() string {
	if d := strings.TrimSpace(os.Getenv("HYPRVALET_PROJECTS_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, "proyectos")
}

// claudeCmd is the command that opens Claude Code; override with HYPRVALET_CLAUDE_CMD.
func claudeCmd() string {
	if c := strings.TrimSpace(os.Getenv("HYPRVALET_CLAUDE_CMD")); c != "" {
		return c
	}
	return "claude"
}

// terminalCmd is the terminal used to host Claude Code; override with HYPRVALET_TERMINAL.
func terminalCmd() string {
	if t := strings.TrimSpace(os.Getenv("HYPRVALET_TERMINAL")); t != "" {
		return t
	}
	return "alacritty"
}

var slugRuns = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns a spoken project name into a single safe path segment:
// lowercase, non-alphanumerics collapsed to dashes, trimmed. The result can
// contain no slash, dot, or shell metacharacter, so joining it to the base
// directory cannot escape it. An empty result is rejected by the caller.
func slugify(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = slugRuns.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// resolve validates a project name and returns its slug and full path.
func resolve(rawName string) (slug, dir string, err error) {
	slug = slugify(rawName)
	if slug == "" {
		return "", "", core.Validationf("missing or unusable project name %q — say a name with letters or numbers", rawName)
	}
	return slug, filepath.Join(baseDir(), slug), nil
}

// SessionPrefix names the tmux sessions that host Claude Code, one per project.
// Running Claude inside tmux is what lets the assistant read the terminal back
// (tmux capture-pane); the prefix lets it find those sessions among any others.
const SessionPrefix = "hyprvalet-"

// SessionFor is the tmux session name for a project slug.
func SessionFor(slug string) string { return SessionPrefix + slug }

// openClaude launches a terminal running Claude Code inside a named tmux
// session (so the terminal is readable later), fire-and-forget: the editor
// outlives the turn, so the process is detached from the request and reaped in
// the background. tmux new-session -A attaches to the session if it already
// exists, so reopening a project returns to the same running Claude.
func openClaude(slug, dir string) error {
	cmd := exec.Command(terminalCmd(), "--working-directory", dir, "-e",
		"tmux", "new-session", "-A", "-s", SessionFor(slug), "-c", dir, claudeCmd())
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("opening Claude Code: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// newProject creates a project folder and opens Claude Code in it.
type newProject struct{}

func (newProject) ID() string { return "project.new" }
func (newProject) Description() string {
	return "Create a new project folder and open Claude Code in it"
}
func (newProject) Access() core.AccessKind { return core.AccessCommand }
func (newProject) Risk() core.Risk         { return core.RiskConfirm }
func (newProject) Params() []string        { return []string{"name"} }
func (newProject) Run(_ context.Context, args core.Args) (string, error) {
	slug, dir, err := resolve(args["name"])
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		return "", core.Validationf("a project named %q already exists — use project.open to reopen it", slug)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating project folder: %w", err)
	}
	if err := openClaude(slug, dir); err != nil {
		return "", err
	}
	return fmt.Sprintf("created project %q and opened Claude Code", slug), nil
}

// openProject opens Claude Code in an existing project folder.
type openProject struct{}

func (openProject) ID() string              { return "project.open" }
func (openProject) Description() string     { return "Open Claude Code in an existing project folder" }
func (openProject) Access() core.AccessKind { return core.AccessCommand }
func (openProject) Risk() core.Risk         { return core.RiskConfirm }
func (openProject) Params() []string        { return []string{"name"} }
func (openProject) Run(_ context.Context, args core.Args) (string, error) {
	slug, dir, err := resolve(args["name"])
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		return "", core.Validationf("no project named %q under %s", slug, baseDir())
	}
	if err := openClaude(slug, dir); err != nil {
		return "", err
	}
	return fmt.Sprintf("opened Claude Code in project %q", slug), nil
}
