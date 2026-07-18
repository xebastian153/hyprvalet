package policyfile

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
)

// ActionLogPath returns where the recent-action history for doom-loop detection
// is persisted: under $XDG_RUNTIME_DIR (wiped on logout), falling back to a
// per-user temp directory. It survives across CLI invocations within a session
// so a loop spread over many separate runs is still caught.
func ActionLogPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("hyprvalet-%d", os.Getuid()))
	}
	return filepath.Join(dir, "hyprvalet", "action-log.json")
}

type fileRecord struct {
	Signature string `json:"signature"`
	At        string `json:"at"`
}

// LoadActionLog reads the recent-action history. A missing file is an empty log,
// not an error; a corrupt individual record is skipped.
func LoadActionLog(path string) ([]core.ActionRecord, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var raw []fileRecord
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	out := make([]core.ActionRecord, 0, len(raw))
	for _, r := range raw {
		at, err := time.Parse(time.RFC3339, r.At)
		if err != nil {
			continue // skip a corrupt record rather than failing the load
		}
		out = append(out, core.ActionRecord{Signature: r.Signature, At: at})
	}
	return out, nil
}

// SaveActionLog writes the history. The directory is created 0700 and the file
// 0600 — it records what the user has been doing and is theirs alone.
func SaveActionLog(path string, history []core.ActionRecord) error {
	raw := make([]fileRecord, 0, len(history))
	for _, r := range history {
		raw = append(raw, fileRecord{Signature: r.Signature, At: r.At.UTC().Format(time.RFC3339)})
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}
