// Package tts speaks text aloud by driving the local piper CLI and playing the
// result through PipeWire. Like mic and whisper it is a frontend adapter: the
// spoken reply is rendered entirely at the edge, and nothing behind the text
// boundary knows a speaker exists. Speech is an output garnish, never a gate —
// every failure here is reported and swallowed, the action's outcome stands.
package tts

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

// Client renders speech with a piper binary and a voice model.
type Client struct {
	bin   string
	play  string
	voice string
}

// DefaultVoicePath is where hyprvalet looks for the voice model unless
// HYPRVALET_VOICE overrides it.
func DefaultVoicePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "share", "hyprvalet", "en_US-lessac-medium.onnx")
}

// Default builds a client from the environment: piper-tts (the binary name the
// Arch package ships) and pw-play on PATH, and the default voice, overridable
// with HYPRVALET_VOICE.
func Default() *Client {
	voice := strings.TrimSpace(os.Getenv("HYPRVALET_VOICE"))
	if voice == "" {
		voice = DefaultVoicePath()
	}
	return New("piper-tts", "pw-play", voice)
}

// New returns a client for specific binaries and voice model. Tests inject
// stubs here.
func New(bin, play, voice string) *Client {
	return &Client{bin: bin, play: play, voice: voice}
}

// Speak renders text to a temporary WAV and plays it, blocking until playback
// ends. A missing voice or binary fails with a hint; callers treat any error as
// "stay silent", never as a reason to change behavior.
func (c *Client) Speak(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if _, err := os.Stat(c.voice); err != nil {
		return fmt.Errorf("voice model not found at %s (set HYPRVALET_VOICE or download one from rhasspy/piper-voices)", c.voice)
	}

	wav := filepath.Join(os.TempDir(), fmt.Sprintf("hyprvalet-say-%d.wav", os.Getpid()))
	defer os.Remove(wav)

	render := exec.CommandContext(ctx, c.bin, "--model", c.voice, "--output_file", wav)
	render.Stdin = strings.NewReader(text)
	var stderr bytes.Buffer
	render.Stderr = &stderr
	if err := render.Run(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return fmt.Errorf("%s not found — install piper (e.g. 'yay -S piper-tts-bin')", c.bin)
		}
		return fmt.Errorf("speech synthesis failed: %v: %s", err, strings.TrimSpace(stderr.String()))
	}

	playArgs := []string{}
	if t := strings.TrimSpace(os.Getenv("HYPRVALET_PLAY_TARGET")); t != "" {
		playArgs = append(playArgs, "--target", t)
	}
	playArgs = append(playArgs, wav)
	if err := exec.CommandContext(ctx, c.play, playArgs...).Run(); err != nil {
		return fmt.Errorf("playback failed: %w", err)
	}
	return nil
}
