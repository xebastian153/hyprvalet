package stt

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/xebastian153/hyprvalet/internal/adapters/groqstt"
	"github.com/xebastian153/hyprvalet/internal/adapters/whisper"
)

func makeWav(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.wav")
	if err := os.WriteFile(p, []byte("fake wav"), 0o600); err != nil {
		t.Fatalf("write wav: %v", err)
	}
	return p
}

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

func TestTranscribeWithClient_CloudSuccess(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "cloud hello"})
	}))
	defer ts.Close()

	groqClient := groqstt.New(ts.URL, groqstt.DefaultModel, "sk-test")
	bin, model := stubWhisper(t, "local hello")
	whisperClient := whisper.New(bin, model)

	wav := makeWav(t)
	got, err := TranscribeWithClient(context.Background(), wav, groqClient, whisperClient)
	if err != nil {
		t.Fatalf("TranscribeWithClient: %v", err)
	}
	if got != "cloud hello" {
		t.Fatalf("got %q, want cloud hello", got)
	}
}

func TestTranscribeWithClient_FallbackToLocal(t *testing.T) {
	// Groq returns 500, should fallback to whisper stub.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal", http.StatusInternalServerError)
	}))
	defer ts.Close()

	groqClient := groqstt.New(ts.URL, groqstt.DefaultModel, "k")
	bin, model := stubWhisper(t, "fallback hello")
	whisperClient := whisper.New(bin, model)

	wav := makeWav(t)
	got, err := TranscribeWithClient(context.Background(), wav, groqClient, whisperClient)
	if err != nil {
		t.Fatalf("fallback should succeed: %v", err)
	}
	if got != "fallback hello" {
		t.Fatalf("got %q, want fallback hello", got)
	}
}

func TestTranscribe_NoGroqKey_Fallback(t *testing.T) {
	t.Setenv("GROQ_API_KEY", "")
	bin, model := stubWhisper(t, "local only")
	// Ensure EnvFilePath not interfering; just test TranscribeWithClient with nil groq.
	whisperClient := whisper.New(bin, model)
	wav := makeWav(t)
	got, err := TranscribeWithClient(context.Background(), wav, nil, whisperClient)
	if err != nil {
		t.Fatalf("TranscribeWithClient nil groq: %v", err)
	}
	if got != "local only" {
		t.Fatalf("got %q, want local only", got)
	}
}
