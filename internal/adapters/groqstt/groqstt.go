// Package groqstt transcribes speech via Groq's cloud Whisper API.
// It is a frontend adapter: voice becomes text HERE, at the edge, and
// everything past this boundary — reasoning, permission gate, execution —
// sees only the same natural-language text a typed command would produce.
// The core never knows a microphone or a cloud exists.
//
// Resilience is by composition: when GROQ_API_KEY is set this adapter is the
// primary STT; when it fails (no network, bad key, rate-limited) the caller
// falls back to the local whisper.cpp adapter. Silence and artifacts are
// cleaned exactly like whisper.CleanTranscript so both paths behave identically.
package groqstt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultBaseURL is the Groq OpenAI-compatible API root.
const DefaultBaseURL = "https://api.groq.com/openai/v1"

// DefaultModel is the Groq STT model.
const DefaultModel = "whisper-large-v3"

// Client talks to Groq's /audio/transcriptions endpoint.
type Client struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

// New returns a client for a specific endpoint, model, and key.
// Tests inject a mock server URL here; production uses NewFromEnv.
func New(baseURL, model, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  strings.TrimSpace(apiKey),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// NewFromEnv builds a client from the environment.
// GROQ_API_KEY is required; HYPRVALET_GROQ_URL and HYPRVALET_GROQ_STT_MODEL
// override the defaults.
func NewFromEnv() *Client {
	return New(
		envOr("HYPRVALET_GROQ_URL", DefaultBaseURL),
		envOr("HYPRVALET_GROQ_STT_MODEL", DefaultModel),
		os.Getenv("GROQ_API_KEY"),
	)
}

// Available reports whether Groq STT can be used: a key is configured.
func Available() bool {
	return strings.TrimSpace(os.Getenv("GROQ_API_KEY")) != ""
}

// EnvFilePath returns the path to the 0600 env file containing GROQ_API_KEY.
// Respects XDG_CONFIG_HOME, else ~/.config/hyprvalet/env.
func EnvFilePath() string {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, "hyprvalet", "env")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(os.TempDir(), "hyprvalet-env")
	}
	return filepath.Join(home, ".config", "hyprvalet", "env")
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// Transcribe converts one WAV recording (16 kHz mono) to text via Groq.
// Language is auto by default; HYPRVALET_STT_LANG pins it (e.g. "es") —
// auto-detection guesses wrong on one-word utterances like a spoken "sí".
//
// If GROQ_API_KEY is empty it returns an error containing
// "groq api key not found at ..." so the caller can fallback to whisper.cpp
// without treating a missing key as a hard failure.
func (c *Client) Transcribe(ctx context.Context, wavPath string) (string, error) {
	if strings.TrimSpace(c.apiKey) == "" {
		return "", fmt.Errorf("groq api key not found at %s — set GROQ_API_KEY in ~/.config/hyprvalet/env (0600) or environment", EnvFilePath())
	}

	if _, err := os.Stat(wavPath); err != nil {
		return "", fmt.Errorf("audio file not found at %s: %w", wavPath, err)
	}

	lang := strings.TrimSpace(os.Getenv("HYPRVALET_STT_LANG"))
	if lang == "" {
		lang = "auto"
	}

	// Build multipart/form-data body.
	var body bytes.Buffer
	w := multipart.NewWriter(&body)

	// Model, language, response_format, temperature.
	if err := w.WriteField("model", c.model); err != nil {
		return "", fmt.Errorf("writing model field: %w", err)
	}
	// Groq expects language as ISO code; "auto" means omit detection.
	// Spec says to send "auto" or HYPRVALET_STT_LANG explicitly, so we always send it.
	if err := w.WriteField("language", lang); err != nil {
		return "", fmt.Errorf("writing language field: %w", err)
	}
	if err := w.WriteField("response_format", "json"); err != nil {
		return "", fmt.Errorf("writing response_format field: %w", err)
	}
	if err := w.WriteField("temperature", "0"); err != nil {
		return "", fmt.Errorf("writing temperature field: %w", err)
	}

	// File part: audio/wav.
	file, err := os.Open(wavPath)
	if err != nil {
		return "", fmt.Errorf("opening audio file: %w", err)
	}
	defer file.Close()

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="%s"`, escapeQuotes(filepath.Base(wavPath))))
	h.Set("Content-Type", "audio/wav")
	part, err := w.CreatePart(h)
	if err != nil {
		return "", fmt.Errorf("creating file part: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("writing file part: %w", err)
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("closing multipart writer: %w", err)
	}

	url := c.baseURL + "/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	client := c.http
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("calling groq stt at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(b))
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return "", fmt.Errorf("groq: unauthorized (401): %s", msg)
		case http.StatusTooManyRequests:
			return "", fmt.Errorf("groq: rate limited (429): %s (retryable)", msg)
		default:
			if resp.StatusCode >= 500 {
				return "", fmt.Errorf("groq: server error (%s): %s (retryable)", resp.Status, msg)
			}
			return "", fmt.Errorf("groq returned %s: %s", resp.Status, msg)
		}
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decoding groq stt response: %w", err)
	}

	return CleanTranscript(out.Text), nil
}

// CleanTranscript normalizes STT output into one request line: joins lines,
// collapses whitespace, and strips artifacts like "[BLANK_AUDIO]" so silence
// transcribes to an empty string instead of a fake request.
// Mirrors whisper.CleanTranscript so both STT backends behave identically.
func CleanTranscript(raw string) string {
	var parts []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			continue
		}
		parts = append(parts, line)
	}
	return strings.Join(parts, " ")
}

func escapeQuotes(s string) string {
	return strings.ReplaceAll(s, `"`, `_`)
}
