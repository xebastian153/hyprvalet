package policyfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// SessionAllowPath returns where session-wide "always allow" grants are
// persisted: under $XDG_RUNTIME_DIR (wiped on logout, so a grant never outlives
// the session), falling back to a per-user temp directory.
func SessionAllowPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("hyprvalet-%d", os.Getuid()))
	}
	return filepath.Join(dir, "hyprvalet", "session-allow.json")
}

// LoadSessionAllow reads the set of session-granted capability ids. A missing
// file is an empty set, not an error.
func LoadSessionAllow(path string) (core.SessionAllow, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return core.SessionAllow{}, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	s := core.SessionAllow{}
	for _, id := range ids {
		s[id] = true
	}
	return s, nil
}

// SaveSessionAllow writes the grant set as a sorted id list. The directory is
// created 0700 and the file 0600 — session grants are per-user and privileged.
func SaveSessionAllow(path string, s core.SessionAllow) error {
	ids := make([]string, 0, len(s))
	for id := range s {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
