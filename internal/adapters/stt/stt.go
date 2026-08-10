// Package stt composes cloud and local speech-to-text into one resilient
// transcription path: try Groq's cloud Whisper (whisper-large-v3) when
// GROQ_API_KEY is set, and fall back to the local whisper.cpp adapter when
// cloud is unavailable — no network, bad key, rate-limited, or timeout.
// The agent degrades in availability, never silently in quality: a degraded
// transcription still works, and the cause is visible on stderr.
//
// Hexagonal purity: this adapter lives at the edge and composes two other
// adapters (groqstt + whisper). The core never imports it; the CLI and daemon
// decide to call it instead of calling whisper directly.
package stt

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/xebastian153/hyprvalet/internal/adapters/groqstt"
	"github.com/xebastian153/hyprvalet/internal/adapters/whisper"
)

// Transcribe converts wavPath to text, cloud-first.
// When GROQ_API_KEY is empty it goes straight to local whisper.cpp.
// When Groq fails it logs the cloud error and retries locally.
func Transcribe(ctx context.Context, wavPath string) (string, error) {
	if key := strings.TrimSpace(os.Getenv("GROQ_API_KEY")); key != "" {
		c := groqstt.NewFromEnv()
		txt, err := c.Transcribe(ctx, wavPath)
		if err == nil {
			return txt, nil
		}
		// Cloud failed — degrade visibly, then try local.
		fmt.Fprintf(os.Stderr, "stt: groq cloud transcription failed (%v) — falling back to local whisper.cpp\n", err)
	}
	return whisper.Default().Transcribe(ctx, wavPath)
}

// TranscribeWithClient is the testable core: tries groqClient first when
// non-nil, then falls back to the whisper client. Production uses Transcribe.
func TranscribeWithClient(ctx context.Context, wavPath string, groqClient *groqstt.Client, whisperClient *whisper.Client) (string, error) {
	if groqClient != nil {
		txt, err := groqClient.Transcribe(ctx, wavPath)
		if err == nil {
			return txt, nil
		}
		fmt.Fprintf(os.Stderr, "stt: groq cloud transcription failed (%v) — falling back to local whisper.cpp\n", err)
	}
	return whisperClient.Transcribe(ctx, wavPath)
}

// TranscribeWithBackend converts wavPath to text and reports which backend
// succeeded, for per-step timing labels (e.g. "groq" or "whisper"). It
// preserves the same cloud-first fallback. The provider name only is returned
// so the timing line `STT (groq): 465ms` matches the spec without double
// parentheses; the model is visible in debug logs if needed.
func TranscribeWithBackend(ctx context.Context, wavPath string) (string, string, error) {
	if key := strings.TrimSpace(os.Getenv("GROQ_API_KEY")); key != "" {
		c := groqstt.NewFromEnv()
		txt, err := c.Transcribe(ctx, wavPath)
		if err == nil {
			return txt, "groq", nil
		}
		fmt.Fprintf(os.Stderr, "stt: groq cloud transcription failed (%v) — falling back to local whisper.cpp\n", err)
	}
	wc := whisper.Default()
	txt, err := wc.Transcribe(ctx, wavPath)
	if err != nil {
		return "", "", err
	}
	return txt, "whisper", nil
}

// TranscribeWithClientAndBackend is the testable variant that also returns the
// backend label, for unit tests that inject stub clients.
func TranscribeWithClientAndBackend(ctx context.Context, wavPath string, groqClient *groqstt.Client, whisperClient *whisper.Client) (string, string, error) {
	if groqClient != nil {
		txt, err := groqClient.Transcribe(ctx, wavPath)
		if err == nil {
			return txt, "groq", nil
		}
		fmt.Fprintf(os.Stderr, "stt: groq cloud transcription failed (%v) — falling back to local whisper.cpp\n", err)
	}
	txt, err := whisperClient.Transcribe(ctx, wavPath)
	if err != nil {
		return "", "", err
	}
	return txt, "whisper", nil
}
