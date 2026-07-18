package memory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// engramProject namespaces the assistant's observations inside its own Engram
// database — a second wall of separation on top of the isolated data directory.
const engramProject = "jarvis"

// engramTimeout bounds a single engram invocation. Memory is a garnish; it must
// never hang a conversation waiting on a subprocess.
const engramTimeout = 6 * time.Second

// engramStore fulfils the Store port by shelling out to the `engram` binary —
// the same pattern the desktop adapters use for hyprctl and tmux. Every call is
// pinned to a private data directory via ENGRAM_DATA_DIR, so the assistant's
// memory lives in its own SQLite file and can never read or write the Engram a
// user runs for other agents.
type engramStore struct {
	dataDir string
	project string
}

func newEngramStore() engramStore {
	return engramStore{dataDir: engramDataDir(), project: engramProject}
}

// engramDataDir is the assistant's private Engram home:
// $XDG_DATA_HOME/hyprvalet/engram, else ~/.local/share/hyprvalet/engram.
func engramDataDir() string {
	dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "hyprvalet", "engram")
}

// run invokes engram with the private data directory forced into its
// environment, so no ambient ENGRAM_DATA_DIR or default ~/.engram is ever used.
func (s engramStore) run(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), engramTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "engram", args...)
	cmd.Env = append(os.Environ(), "ENGRAM_DATA_DIR="+s.dataDir)
	out, err := cmd.Output()
	return string(out), err
}

// Remember saves one observation. The full note is the message; a truncated
// copy is the title, which is what Engram shows in listings and its TUI.
func (s engramStore) Remember(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	_, err := s.run("save", title(text), text, "--type", "note", "--project", s.project)
	return err
}

// Search returns notes relevant to the query. Engram's full-text search ANDs
// every term and does not stem, so a spoken "what database do I prefer" would
// miss a note about "databases". To recall the way a person would, the query is
// broken into its meaningful words and each is searched on its own; the union
// (deduplicated, newest kept) is the result. This turns Engram's strict AND into
// the forgiving OR a conversation needs.
func (s engramStore) Search(query string, n int) ([]Entry, error) {
	terms := content(query)
	if len(terms) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	var hits []Entry
	for _, term := range terms {
		out, err := s.run("search", term, "--project", s.project, "--limit", strconv.Itoa(n))
		if err != nil {
			continue // a single failed term must not sink the whole recall
		}
		for _, e := range parseSearch(out) {
			key := strings.ToLower(e.Text)
			if e.Text == "" || seen[key] {
				continue
			}
			seen[key] = true
			hits = append(hits, e)
			if n > 0 && len(hits) >= n {
				return hits, nil
			}
		}
	}
	return hits, nil
}

// Recent returns the most recent notes, read from `engram context`.
func (s engramStore) Recent(n int) ([]Entry, error) {
	out, err := s.run("context", s.project)
	if err != nil {
		return nil, nil // an empty memory is not an error
	}
	return parseContext(out, n), nil
}

// Forget deletes every note relevant to the query. It reuses the same
// word-union recall to find candidates, then soft-deletes each by its Engram id.
func (s engramStore) Forget(query string) (int, error) {
	terms := content(query)
	if len(terms) == 0 {
		return 0, nil
	}
	ids := map[int]bool{}
	for _, term := range terms {
		out, err := s.run("search", term, "--project", s.project, "--limit", "50")
		if err != nil {
			continue
		}
		for _, id := range parseIDs(out) {
			ids[id] = true
		}
	}
	removed := 0
	for id := range ids {
		if _, err := s.run("delete", strconv.Itoa(id)); err == nil {
			removed++
		}
	}
	return removed, nil
}

// title is the short label Engram lists a note under: the first line, clipped.
func title(text string) string {
	text = strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	const max = 60
	if len([]rune(text)) > max {
		return string([]rune(text)[:max])
	}
	if text == "" {
		return "note"
	}
	return text
}

var (
	// searchHeader matches an Engram search hit header:
	//   [1] #7 (preference) — Preferencia de BD
	searchHeader = regexp.MustCompile(`^\[\d+\]\s+#(\d+)\s+\([^)]*\)\s+[—-]\s+(.*)$`)
	// metaLine matches the trailing "date | project: … | scope: …" line.
	metaLine = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}.*\|\s*project:`)
	// contextObs matches a recent-observation bullet from `engram context`:
	//   - [note] **Title**: the message
	contextObs = regexp.MustCompile(`^-\s+\[[^\]]*\]\s+\*\*(.*?)\*\*:\s*(.*)$`)
	// idRef matches the "#7" observation id in any engram output.
	idRef = regexp.MustCompile(`#(\d+)`)
)

// parseSearch pulls notes out of `engram search` text. For each hit it prefers
// the message line (the full note) and falls back to the header title.
func parseSearch(out string) []Entry {
	if strings.Contains(out, "No memories found") {
		return nil
	}
	lines := strings.Split(out, "\n")
	var entries []Entry
	for i := 0; i < len(lines); i++ {
		m := searchHeader.FindStringSubmatch(strings.TrimRight(lines[i], " \t"))
		if m == nil {
			continue
		}
		text := strings.TrimSpace(m[2]) // title fallback
		// The message is the next indented, non-metadata, non-blank line.
		for j := i + 1; j < len(lines); j++ {
			body := strings.TrimSpace(lines[j])
			if body == "" || searchHeader.MatchString(strings.TrimSpace(lines[j])) {
				break
			}
			if metaLine.MatchString(body) {
				continue
			}
			text = body
			break
		}
		entries = append(entries, Entry{Text: text})
	}
	return entries
}

// parseContext pulls up to n notes from the "Recent Observations" section of
// `engram context`, returning their messages.
func parseContext(out string, n int) []Entry {
	lines := strings.Split(out, "\n")
	inObs := false
	var entries []Entry
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "### ") {
			inObs = strings.Contains(trimmed, "Recent Observations")
			continue
		}
		if !inObs {
			continue
		}
		if m := contextObs.FindStringSubmatch(trimmed); m != nil {
			text := strings.TrimSpace(m[2])
			if text == "" {
				text = strings.TrimSpace(m[1]) // title fallback
			}
			entries = append(entries, Entry{Text: text})
			if n > 0 && len(entries) >= n {
				break
			}
		}
	}
	return entries
}

// parseIDs returns every observation id referenced in engram output.
func parseIDs(out string) []int {
	var ids []int
	for _, m := range idRef.FindAllStringSubmatch(out, -1) {
		if id, err := strconv.Atoi(m[1]); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}
