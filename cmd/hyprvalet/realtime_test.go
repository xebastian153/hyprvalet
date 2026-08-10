package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsRealtimeEnabled(t *testing.T) {
	tests := []struct {
		name string
		val  string
		want bool
	}{
		{"empty defaults off", "", false},
		{"on", "on", true},
		{"ON", "ON", true},
		{"1", "1", true},
		{"true", "true", true},
		{"off", "off", false},
		{"random", "maybe", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HYPRVALET_REALTIME", tt.val)
			if got := isRealtimeEnabled(); got != tt.want {
				t.Fatalf("isRealtimeEnabled %q = %v want %v", tt.val, got, tt.want)
			}
		})
	}
}

func TestRealtimeLLMFlag(t *testing.T) {
	tests := []struct {
		val  string
		want string
	}{
		{"", "cerebras"},
		{"cerebras", "cerebras"},
		{"ollama", "ollama"},
		{"OLLAMA", "ollama"},
		{"unknown", "cerebras"},
	}
	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			t.Setenv("HYPRVALET_REALTIME_LLM", tt.val)
			if got := realtimeLLM(); got != tt.want {
				t.Fatalf("realtimeLLM %q = %q want %q", tt.val, got, tt.want)
			}
		})
	}
}

func TestHasCerebrasKeyPrivacy0600(t *testing.T) {
	// Uses helper that checks env file perms via realtime.HasCerebrasKeyWithPath
	// Main's hasCerebrasKey delegates to realtime.HasCerebrasKey, but we test
	// the file-perm logic via the exported helper in realtime package indirectly.
	// Here we test main's wrapper with env var isolation: when file is 0644, should be false.
	t.Run("key with 0600 file true", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "env")
		if err := os.WriteFile(path, []byte("CEREBRAS_API_KEY=sk-0600\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Set both env var and redirect EnvFilePath via XDG_CONFIG_HOME to our temp dir
		t.Setenv("CEREBRAS_API_KEY", "sk-0600")
		t.Setenv("XDG_CONFIG_HOME", dir)
		// Need to ensure file is at $XDG_CONFIG_HOME/hyprvalet/env
		envDir := filepath.Join(dir, "hyprvalet")
		if err := os.MkdirAll(envDir, 0o700); err != nil {
			t.Fatal(err)
		}
		envFile := filepath.Join(envDir, "env")
		if err := os.WriteFile(envFile, []byte("CEREBRAS_API_KEY=sk-0600\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if !hasCerebrasKey() {
			t.Fatalf("expected true for 0600 file")
		}
	})

	t.Run("key with 0644 file false privacy", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("CEREBRAS_API_KEY", "sk-0644")
		t.Setenv("XDG_CONFIG_HOME", dir)
		envDir := filepath.Join(dir, "hyprvalet")
		if err := os.MkdirAll(envDir, 0o700); err != nil {
			t.Fatal(err)
		}
		envFile := filepath.Join(envDir, "env")
		if err := os.WriteFile(envFile, []byte("CEREBRAS_API_KEY=sk-0644\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(envFile, 0o644); err != nil {
			t.Fatal(err)
		}
		if hasCerebrasKey() {
			t.Fatalf("expected false for 0644 file")
		}
	})

	t.Run("no key false", func(t *testing.T) {
		t.Setenv("CEREBRAS_API_KEY", "")
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if hasCerebrasKey() {
			t.Fatalf("expected false when no key")
		}
	})
}

func TestShouldUseRealtimeGate(t *testing.T) {
	tests := []struct {
		name    string
		enabled string
		want    bool
	}{
		{"off", "off", false},
		{"empty", "", false},
		{"on", "on", true},
		{"1", "1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HYPRVALET_REALTIME", tt.enabled)
			// key not needed for gate anymore; ShouldUseRealtime true even without key (fallback to ollama)
			if got := shouldUseRealtime(); got != tt.want {
				t.Fatalf("shouldUseRealtime %q = %v want %v", tt.enabled, got, tt.want)
			}
		})
	}
}

func TestRealtimeFallbackReasonerNotNil(t *testing.T) {
	// buildRealtimeReasoner should never be nil; it must return a fallback that
	// degrades to batch (Ollama http://localhost:11434/v1) when key missing
	// This is the wiring test for fallback.New(realtime, batch)
	r := buildRealtimeReasoner(false, false)
	if r == nil {
		t.Fatal("buildRealtimeReasoner returned nil")
	}
	r2 := buildRealtimeReasoner(true, true)
	if r2 == nil {
		t.Fatal("buildRealtimeReasoner strong returned nil")
	}
}

func TestWakeGatedRealtimeOnlyAfterWake(t *testing.T) {
	// Spec: MUST NOT open ws://127.0.0.1:8765/v1/realtime until wake-word match.
	// We test the pure helper isWakeGatedRealtime: it returns true only when
	// shouldUseRealtime && woken.
	tests := []struct {
		name    string
		enabled string
		woken   bool
		want    bool
	}{
		{"disabled not woken", "off", false, false},
		{"disabled woken still false", "off", true, false},
		{"enabled not woken no dial", "on", false, false},
		{"enabled woken dial", "on", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HYPRVALET_REALTIME", tt.enabled)
			if got := isWakeGatedRealtime(tt.woken); got != tt.want {
				t.Fatalf("isWakeGatedRealtime enabled=%q woken=%v = %v want %v", tt.enabled, tt.woken, got, tt.want)
			}
		})
	}
}
