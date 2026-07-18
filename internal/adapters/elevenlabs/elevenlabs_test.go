package elevenlabs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func stubPlayer(t *testing.T) (play, marker string) {
	t.Helper()
	dir := t.TempDir()
	marker = filepath.Join(dir, "played")
	play = filepath.Join(dir, "play-stub")
	if err := os.WriteFile(play, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	return play, marker
}

func TestSpeakCallsAPIAndPlays(t *testing.T) {
	var gotPath, gotKey string
	var gotBody map[string]string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("xi-api-key")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte("fake-mp3-bytes"))
	}))
	defer ts.Close()

	play, marker := stubPlayer(t)
	c := New(ts.URL, "sk-test", "voice123", "eleven_multilingual_v2", play)
	if err := c.Speak(context.Background(), "hola"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if gotPath != "/text-to-speech/voice123" || gotKey != "sk-test" {
		t.Fatalf("path=%q key=%q", gotPath, gotKey)
	}
	if gotBody["text"] != "hola" || gotBody["model_id"] != "eleven_multilingual_v2" {
		t.Fatalf("body = %v", gotBody)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("playback was never invoked")
	}
}

func TestSpeakEmptyIsSilentNoOp(t *testing.T) {
	play, marker := stubPlayer(t)
	c := New("http://unused", "k", "v", "m", play)
	if err := c.Speak(context.Background(), "  "); err != nil {
		t.Fatalf("Speak(empty): %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("empty text must not reach playback")
	}
}

func TestSpeakMissingKeyFailsFast(t *testing.T) {
	play, _ := stubPlayer(t)
	c := New("http://unused", "", "v", "m", play)
	if err := c.Speak(context.Background(), "x"); err == nil {
		t.Fatal("missing key must error before any network call")
	}
}

func TestSpeakAPIErrorSurfaces(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"quota exceeded"}`, http.StatusUnauthorized)
	}))
	defer ts.Close()
	play, _ := stubPlayer(t)
	if err := New(ts.URL, "k", "v", "m", play).Speak(context.Background(), "x"); err == nil {
		t.Fatal("a non-200 response must be an error (so the chain falls through)")
	}
}
