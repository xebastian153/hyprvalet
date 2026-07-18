package whisper

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanTranscript(t *testing.T) {
	tests := []struct {
		name, raw, want string
	}{
		{"joins lines", " switch to workspace 3\n and open firefox \n", "switch to workspace 3 and open firefox"},
		{"strips artifacts", "[BLANK_AUDIO]\n", ""},
		{"keeps text around artifacts", "[MUSIC]\nopen firefox\n", "open firefox"},
		{"empty is empty", "\n\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CleanTranscript(tt.raw); got != tt.want {
				t.Fatalf("CleanTranscript(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

// stubWhisper writes an executable that prints a canned transcript, standing in
// for whisper-cli so the exec path is tested without models or audio.
func stubWhisper(t *testing.T, output string) (bin, model string) {
	t.Helper()
	dir := t.TempDir()
	bin = filepath.Join(dir, "whisper-stub")
	script := "#!/bin/sh\nprintf '" + output + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	model = filepath.Join(dir, "model.bin")
	if err := os.WriteFile(model, []byte("fake"), 0o600); err != nil {
		t.Fatalf("writing model: %v", err)
	}
	return bin, model
}

func TestTranscribeRunsBinary(t *testing.T) {
	bin, model := stubWhisper(t, ` switch to workspace 3\n`)
	got, err := New(bin, model).Transcribe(context.Background(), "ignored.wav")
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if got != "switch to workspace 3" {
		t.Fatalf("transcript = %q", got)
	}
}

func TestTranscribeMissingModelHints(t *testing.T) {
	c := New("whisper-cli", filepath.Join(t.TempDir(), "nope.bin"))
	if _, err := c.Transcribe(context.Background(), "x.wav"); err == nil {
		t.Fatal("missing model must error with a download hint")
	}
}

func TestTranscribeMissingBinaryHints(t *testing.T) {
	_, model := stubWhisper(t, "x")
	c := New("definitely-not-installed-anywhere", model)
	if _, err := c.Transcribe(context.Background(), "x.wav"); err == nil {
		t.Fatal("missing binary must error with an install hint")
	}
}
