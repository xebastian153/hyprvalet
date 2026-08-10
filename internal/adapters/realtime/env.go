package realtime

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RealtimeEnabled reports whether HYPRVALET_REALTIME enables streaming.
// Accepted truthy values: "on", "1", "true", "yes" (case-insensitive, trimmed).
// Default is off — privacy and backward compatibility: no WS dialled unless explicit.
func RealtimeEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("HYPRVALET_REALTIME")))
	switch v {
	case "on", "1", "true", "yes":
		return true
	default:
		return false
	}
}

// RealtimeLLM reports which LLM backend the realtime sidecar should use.
// Valid: "cerebras" (default) or "ollama". Case-insensitive, trimmed.
func RealtimeLLM() string {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("HYPRVALET_REALTIME_LLM")))
	if v == "ollama" {
		return "ollama"
	}
	return "cerebras"
}

// EnvFilePath returns the path to the 0600 env file containing CEREBRAS_API_KEY.
// Respects XDG_CONFIG_HOME, else ~/.config/hyprvalet/env.
func EnvFilePath() string {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "hyprvalet", "env")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		// Fallback to temp-adjacent path; tests use HasCerebrasKeyWithPath for isolation.
		return filepath.Join(os.TempDir(), "hyprvalet-env")
	}
	return filepath.Join(home, ".config", "hyprvalet", "env")
}

// HasCerebrasKey reports whether CEREBRAS_API_KEY is set and, if the env file
// exists, that it is chmod 0600. PCM leaves the machine only when this is true
// (0600 opt-in per spec). Empty key → false.
func HasCerebrasKey() bool {
	return HasCerebrasKeyWithPath(EnvFilePath())
}

// HasCerebrasKeyWithPath is the testable core: checks CEREBRAS_API_KEY env var
// and verifies the file at path (if it exists) is 0600. Env var is authoritative;
// file content is not re-read — the systemd EnvironmentFile already exports it.
func HasCerebrasKeyWithPath(path string) bool {
	key := strings.TrimSpace(os.Getenv("CEREBRAS_API_KEY"))
	if key == "" {
		return false
	}
	if path == "" {
		return true // no file to check, env var alone is opt-in
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true // file missing → env var alone suffices
		}
		fmt.Fprintf(os.Stderr, "realtime: could not stat env file %q: %v — treating as not 0600\n", path, err)
		return false
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		fmt.Fprintf(os.Stderr, "realtime: privacy gate — env file %q is %04o, want 0600; CEREBRAS_API_KEY ignored (PCM stays local, falling back to Ollama http://localhost:11434/v1)\n", path, perm)
		return false
	}
	return true
}

// ShouldUseRealtime reports whether the voice path should attempt realtime.
// Wake-gated still: listenCmd only dials after wake word, but this flag gates
// whether voiceCmd/listenCmd *would* try WS at all. True when HYPRVALET_REALTIME=on
// even if key missing — then the fallback note explains we use Ollama http://localhost:11434/v1.
func ShouldUseRealtime() bool {
	if !RealtimeEnabled() {
		return false
	}
	// Privacy gate handled downstream: if cerebras requested but key not 0600,
	// HasCerebrasKey will be false and the fallback will degrade to Ollama.
	// We still report true so callers can emit the degrade note, not silently stay batch.
	return true
}

// OllamaFallbackURL is the local LLM when Cerebras key is absent or not 0600.
// Matches ollama.Default URL.
const OllamaFallbackURL = "http://localhost:11434"

// RealtimeAvailable reports whether realtime WS dial should be attempted right now.
// It combines the enable flag, LLm choice, and quarantine/file privacy.
func RealtimeAvailable() bool {
	if !ShouldUseRealtime() {
		return false
	}
	llm := RealtimeLLM()
	if llm == "cerebras" && !HasCerebrasKey() {
		// Still available via Ollama fallback, but not via Cerebras.
		// Caller should fallback to Ollama; we report available as true so fallback chain runs.
		// To allow tests to distinguish, we keep true — degrade note will explain.
		return true
	}
	return true
}
