// Package edgetts speaks text through the edge-tts CLI (Microsoft neural
// voices) — the free cloud tier of the speech chain: better than local piper,
// below ElevenLabs. Text goes to Microsoft; the audio comes back.
package edgetts

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

// Client renders speech with the edge-tts CLI and plays it locally.
type Client struct {
	bin   string
	play  string
	voice string
}

// New returns a client for specific binaries and voice. Tests inject stubs.
func New(bin, play, voice string) *Client {
	return &Client{bin: bin, play: play, voice: voice}
}

// Default builds a client from the environment. The voice defaults to an
// Argentine male neural voice; override with HYPRVALET_EDGE_VOICE.
func Default() *Client {
	return New("edge-tts", "mpv", envOr("HYPRVALET_EDGE_VOICE", "es-AR-TomasNeural"))
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// Speak renders text to speech and plays it, blocking until playback ends.
func (c *Client) Speak(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	audio := filepath.Join(os.TempDir(), fmt.Sprintf("hyprvalet-edge-%d.mp3", os.Getpid()))
	defer os.Remove(audio)

	cmd := exec.CommandContext(ctx, c.bin, "--voice", c.voice, "--text", text, "--write-media", audio)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return fmt.Errorf("%s not found — install it (e.g. 'uv tool install edge-tts')", c.bin)
		}
		return fmt.Errorf("edge-tts failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	if err := exec.CommandContext(ctx, c.play, "--really-quiet", audio).Run(); err != nil {
		return fmt.Errorf("playback failed: %w", err)
	}
	return nil
}
