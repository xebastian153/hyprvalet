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

// ArmStatePath returns where temporal arming grants are persisted. It prefers
// $XDG_RUNTIME_DIR — which the session wipes on logout, so a grant never
// outlives a login session — and falls back to a per-user temp directory.
//
// The file is internal machine state, not something the installer edits, so it
// uses JSON (stdlib, no dependency) rather than the TOML of the policy file.
func ArmStatePath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("hyprvalet-%d", os.Getuid()))
	}
	return filepath.Join(dir, "hyprvalet", "armed.json")
}

// LoadArmState reads persisted grants and prunes any expired at now. A missing
// file is an empty state, not an error; a corrupt individual entry is skipped
// rather than failing the whole load.
func LoadArmState(path string, now time.Time) (core.ArmState, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return core.ArmState{}, nil
	}
	if err != nil {
		return nil, err
	}
	raw := map[string]string{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	state := core.ArmState{}
	for id, ts := range raw {
		t, err := time.Parse(time.RFC3339, ts)
		if err != nil {
			continue // skip a corrupt entry instead of failing the load
		}
		state[id] = t
	}
	state.Prune(now)
	return state, nil
}

// SaveArmState prunes expired grants and writes the rest. The directory is
// created 0700 and the file 0600 — arming state is per-user and privileged.
func SaveArmState(path string, state core.ArmState, now time.Time) error {
	state.Prune(now)
	raw := make(map[string]string, len(state))
	for id, t := range state {
		raw[id] = t.UTC().Format(time.RFC3339)
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
