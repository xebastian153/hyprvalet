// Package eventlog persists the audit log of attempted actions as append-only
// JSON lines. It is an adapter: the core knows only the EventStore port, never
// this file format or location. One event per line makes the log greppable,
// crash-tolerant (a torn final line loses one event, not the file), and
// appendable without rewriting history — history is never edited.
package eventlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// Path returns the audit log location: under $XDG_STATE_HOME (persistent,
// survives logout — an audit trail that vanishes with the session is not an
// audit trail), falling back to ~/.local/state.
func Path() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "hyprvalet", "events.jsonl")
}

// Store is a file-backed core.EventStore.
type Store struct {
	path string
}

// New returns a store writing to path. The file and its directory are created
// on first append.
func New(path string) *Store {
	return &Store{path: path}
}

// record is the wire form of one event line.
type record struct {
	At     time.Time         `json:"at"`
	Source string            `json:"source"`
	Kind   string            `json:"kind"`
	Cap    string            `json:"cap"`
	Args   map[string]string `json:"args,omitempty"`
	Detail string            `json:"detail,omitempty"`
}

// Append writes one event as a JSON line at the end of the log. The directory
// is private (0700) and the log user-only (0600): what you asked your desktop
// to do is nobody else's business.
func (s *Store) Append(e core.Event) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating event log directory: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("opening event log: %w", err)
	}
	defer f.Close()
	line, err := json.Marshal(record{
		At:     e.At.UTC(),
		Source: e.Source,
		Kind:   string(e.Kind),
		Cap:    e.Cap,
		Args:   e.Args,
		Detail: e.Detail,
	})
	if err != nil {
		return fmt.Errorf("encoding event: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("appending event: %w", err)
	}
	return nil
}

// Tail returns up to n most recent events, oldest first. A missing log is an
// empty history, not an error. An unparseable line (a torn write from a crash)
// is skipped: the reader of an audit trail must be robust to the very failures
// it exists to explain.
func (s *Store) Tail(n int) ([]core.Event, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("opening event log: %w", err)
	}
	defer f.Close()

	var events []core.Event
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			continue // torn or corrupt line: skip, keep the rest readable
		}
		events = append(events, core.Event{
			At:     r.At,
			Source: r.Source,
			Kind:   core.EventKind(r.Kind),
			Cap:    r.Cap,
			Args:   core.Args(r.Args),
			Detail: r.Detail,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading event log: %w", err)
	}
	if n > 0 && len(events) > n {
		events = events[len(events)-n:]
	}
	return events, nil
}
