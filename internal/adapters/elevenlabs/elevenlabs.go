// Package elevenlabs speaks text through the ElevenLabs TTS API — the natural,
// human-sounding tier of the speech chain. Cloud with credits and a key: the
// caller composes it above free local backends, so running dry degrades the
// voice, never silences it. Only text leaves the machine; the audio comes back.
package elevenlabs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client renders speech through the ElevenLabs API and plays it locally.
type Client struct {
	baseURL string
	key     string
	voice   string
	model   string
	play    string
	http    *http.Client
}

// Available reports whether ElevenLabs can be used at all: a key is configured.
func Available() bool {
	return strings.TrimSpace(os.Getenv("ELEVENLABS_API_KEY")) != ""
}

// New returns a client for specific endpoint, key, voice, model, and player.
// Tests inject a mock server URL and a stub player here.
func New(baseURL, key, voice, model, play string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		voice:   voice,
		model:   model,
		play:    play,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// Default builds a client from the environment. The voice defaults to Brian
// (deep, resonant); override with HYPRVALET_ELEVENLABS_VOICE (a voice ID) and
// HYPRVALET_ELEVENLABS_MODEL (e.g. eleven_flash_v2_5 for half-cost, lower
// latency at slightly lower quality).
func Default() *Client {
	return New(
		envOr("HYPRVALET_ELEVENLABS_URL", "https://api.elevenlabs.io/v1"),
		os.Getenv("ELEVENLABS_API_KEY"),
		envOr("HYPRVALET_ELEVENLABS_VOICE", "nPczCjzI2devNBz1zQrb"), // Brian
		envOr("HYPRVALET_ELEVENLABS_MODEL", "eleven_multilingual_v2"),
		"mpv",
	)
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// Speak renders text to speech via the API and plays it, blocking until
// playback ends.
func (c *Client) Speak(ctx context.Context, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if c.key == "" {
		return fmt.Errorf("ELEVENLABS_API_KEY is not set")
	}

	body, err := json.Marshal(map[string]string{"text": text, "model_id": c.model})
	if err != nil {
		return fmt.Errorf("encoding request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/text-to-speech/"+c.voice, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", c.key)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("calling elevenlabs: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("elevenlabs returned %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	audio := filepath.Join(os.TempDir(), fmt.Sprintf("hyprvalet-el-%d.mp3", os.Getpid()))
	defer os.Remove(audio)
	f, err := os.Create(audio)
	if err != nil {
		return fmt.Errorf("writing audio: %w", err)
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return fmt.Errorf("writing audio: %w", err)
	}
	f.Close()

	if err := exec.CommandContext(ctx, c.play, "--really-quiet", audio).Run(); err != nil {
		return fmt.Errorf("playback failed: %w", err)
	}
	return nil
}
