// Package whisper transcribes recorded speech to text by driving the local
// whisper.cpp CLI. It is a frontend adapter: voice becomes text HERE, at the
// edge, and everything past this boundary — reasoning, permission gate,
// execution — sees only the same natural-language text a typed command would
// produce. The core never knows a microphone exists.
package whisper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Client runs a whisper.cpp binary against a model file.
type Client struct {
	bin   string
	model string
}

// DefaultModelPath is where hyprvalet looks for the transcription model unless
// HYPRVALET_WHISPER_MODEL overrides it.
func DefaultModelPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "share", "hyprvalet", "ggml-base.bin")
}

// Default builds a client from the environment: whisper-cli on PATH and the
// default model location, overridable with HYPRVALET_WHISPER_MODEL.
func Default() *Client {
	model := strings.TrimSpace(os.Getenv("HYPRVALET_WHISPER_MODEL"))
	if model == "" {
		model = DefaultModelPath()
	}
	return New("whisper-cli", model)
}

// New returns a client for a specific binary and model path. Tests inject a
// stub binary here.
func New(bin, model string) *Client {
	return &Client{bin: bin, model: model}
}

// Transcribe converts one WAV recording (16 kHz mono) to text. Language is
// auto-detected, so Spanish and English commands both work. A missing model or
// binary fails with a hint rather than a raw exec error.
func (c *Client) Transcribe(ctx context.Context, wavPath string) (string, error) {
	if _, err := os.Stat(c.model); err != nil {
		return "", fmt.Errorf("whisper model not found at %s — download one, e.g.:\n  curl -L -o %s https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin",
			c.model, c.model)
	}

	// -nt: no timestamps. Language defaults to auto-detection; HYPRVALET_STT_LANG
	// pins it (e.g. "es") — auto-detection guesses wrong on one-word utterances
	// like a spoken "sí", which matters when that word is a confirmation.
	lang := strings.TrimSpace(os.Getenv("HYPRVALET_STT_LANG"))
	if lang == "" {
		lang = "auto"
	}
	cmd := exec.CommandContext(ctx, c.bin, "-m", c.model, "-f", wavPath, "-nt", "-l", lang)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return "", fmt.Errorf("%s not found — install whisper.cpp (e.g. 'sudo pacman -S whisper-cpp')", c.bin)
		}
		return "", fmt.Errorf("transcription failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return CleanTranscript(stdout.String()), nil
}

// CleanTranscript normalizes whisper output into one request line: joins the
// transcript lines, collapses whitespace, and strips artifacts like "[BLANK_AUDIO]"
// so silence transcribes to an empty string instead of a fake request.
func CleanTranscript(raw string) string {
	var parts []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			continue // artifacts like [BLANK_AUDIO], [MUSIC]
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, " ")
}
