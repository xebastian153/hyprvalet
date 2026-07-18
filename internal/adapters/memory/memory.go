// Package memory is hyprvalet's own long-term memory — the assistant's private
// notebook, on disk, that survives across sessions so it can remember you: your
// name, your preferences, the projects you are planning. It is entirely the
// assistant's own store; it has nothing to do with any external memory system.
//
// The store is append-only JSON lines: durable, greppable, and crash-tolerant
// (a torn final line loses one note, not the file).
package memory

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Capabilities returns every memory capability.
func Capabilities() []core.Capability {
	return []core.Capability{remember{}, recall{}, forget{}}
}

// Path is where the assistant keeps its long-term memory:
// $XDG_DATA_HOME/hyprvalet/memory.jsonl, else ~/.local/share/hyprvalet/.
func Path() string {
	dir := strings.TrimSpace(os.Getenv("XDG_DATA_HOME"))
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "hyprvalet", "memory.jsonl")
}

// Entry is one remembered note.
type Entry struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Remember appends a note to the assistant's memory. Blank notes are ignored.
func Remember(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	path := Path()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating memory directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening memory: %w", err)
	}
	defer f.Close()
	line, err := json.Marshal(Entry{At: time.Now().UTC(), Text: text})
	if err != nil {
		return fmt.Errorf("encoding note: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("writing note: %w", err)
	}
	return nil
}

// All returns every remembered note, oldest first. A missing store is an empty
// memory, not an error; a torn line is skipped.
func All() []Entry {
	f, err := os.Open(Path())
	if err != nil {
		return nil
	}
	defer f.Close()
	var entries []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err == nil && strings.TrimSpace(e.Text) != "" {
			entries = append(entries, e)
		}
	}
	return entries
}

// Recent returns up to n most recent notes, oldest first — the compact block
// folded into the reasoning prompt so the assistant answers with what it knows.
func Recent(n int) []Entry {
	all := All()
	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	return all
}

// Search returns notes sharing a meaningful word with the query, most recent
// first. Matching is by word — not substring, so "use" no longer matches
// "user" — with common filler words dropped and short words ignored, so recall
// stays on-topic. A query word matches a note word when they are equal or one
// is a prefix of the other (so "database" finds "databases").
func Search(query string, n int) []Entry {
	terms := content(query)
	if len(terms) == 0 {
		return nil
	}
	all := All()
	var hits []Entry
	for i := len(all) - 1; i >= 0; i-- {
		if overlaps(terms, content(all[i].Text)) {
			hits = append(hits, all[i])
		}
		if n > 0 && len(hits) >= n {
			break
		}
	}
	return hits
}

// stopwords are common filler words carrying no topic, in the languages the
// assistant is used in — dropped so recall matches on subject, not glue.
var stopwords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "what": true,
	"should": true, "would": true, "that": true, "this": true, "have": true,
	"your": true, "you": true, "our": true, "use": true, "using": true,
	"que": true, "los": true, "las": true, "una": true, "unos": true,
	"para": true, "con": true, "por": true, "del": true, "como": true,
	"cual": true, "cuál": true, "tengo": true, "quiero": true, "sobre": true,
}

// content reduces text to its meaningful words: lowercase, letters/digits only,
// at least four characters, and not a stopword.
func content(text string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r >= 0x80)
	}) {
		if len([]rune(f)) >= 4 && !stopwords[f] {
			out = append(out, f)
		}
	}
	return out
}

// overlaps reports whether any query term matches any note term by equality or
// prefix (either direction), so "database" and "databases" match.
func overlaps(query, note []string) bool {
	for _, q := range query {
		for _, w := range note {
			if q == w || (len(q) >= 4 && strings.HasPrefix(w, q)) || (len(w) >= 4 && strings.HasPrefix(q, w)) {
				return true
			}
		}
	}
	return false
}

// rewrite replaces the whole store with entries (used by forget).
func rewrite(entries []Entry) error {
	path := Path()
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].At.Before(entries[j].At) })
	for _, e := range entries {
		line, _ := json.Marshal(e)
		w.Write(append(line, '\n'))
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

// remember stores a fact the user asked the assistant to keep.
type remember struct{}

func (remember) ID() string { return "memory.remember" }
func (remember) Description() string {
	return "Remember a fact for the long term (a preference, a plan, a detail about you)"
}
func (remember) Access() core.AccessKind { return core.AccessCommand }
func (remember) Risk() core.Risk         { return core.RiskSafe }
func (remember) Params() []string        { return []string{"text"} }
func (remember) Run(_ context.Context, args core.Args) (string, error) {
	text := strings.TrimSpace(args["text"])
	if text == "" {
		return "", core.Validationf("missing required arg %q (what to remember)", "text")
	}
	if err := Remember(text); err != nil {
		return "", err
	}
	return "I will remember that", nil
}

// recall retrieves what the assistant remembers about a topic.
type recall struct{}

func (recall) ID() string              { return "memory.recall" }
func (recall) Description() string     { return "Recall what you remember about a topic" }
func (recall) Access() core.AccessKind { return core.AccessCommand }
func (recall) Risk() core.Risk         { return core.RiskSafe }
func (recall) Params() []string        { return []string{"query"} }
func (recall) Run(_ context.Context, args core.Args) (string, error) {
	q := strings.TrimSpace(args["query"])
	if q == "" {
		return "", core.Validationf("missing required arg %q (what to recall)", "query")
	}
	hits := Search(q, 8)
	if len(hits) == 0 {
		return "I don't remember anything about that", nil
	}
	var b strings.Builder
	for _, e := range hits {
		fmt.Fprintf(&b, "- %s\n", e.Text)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// forget erases the assistant's memory of a topic — everything matching, so a
// mistaken or outdated note can be removed by voice.
type forget struct{}

func (forget) ID() string              { return "memory.forget" }
func (forget) Description() string     { return "Forget everything remembered about a topic" }
func (forget) Access() core.AccessKind { return core.AccessCommand }
func (forget) Risk() core.Risk         { return core.RiskConfirm }
func (forget) Params() []string        { return []string{"query"} }
func (forget) Run(_ context.Context, args core.Args) (string, error) {
	q := strings.TrimSpace(args["query"])
	if q == "" {
		return "", core.Validationf("missing required arg %q (what to forget)", "query")
	}
	words := strings.Fields(strings.ToLower(q))
	all := All()
	var kept []Entry
	removed := 0
	for _, e := range all {
		low := strings.ToLower(e.Text)
		match := false
		for _, w := range words {
			if len(w) >= 3 && strings.Contains(low, w) {
				match = true
				break
			}
		}
		if match {
			removed++
		} else {
			kept = append(kept, e)
		}
	}
	if removed == 0 {
		return "there was nothing to forget about that", nil
	}
	if err := rewrite(kept); err != nil {
		return "", err
	}
	return fmt.Sprintf("forgot %d note(s)", removed), nil
}
