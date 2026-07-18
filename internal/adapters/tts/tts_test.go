package tts

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// stubSpeech writes stub piper/play executables and a fake voice model. The
// piper stub records its invocation by creating the output file it was asked
// for; the play stub records the path it was asked to play.
func stubSpeech(t *testing.T) (bin, play, voice, playedMarker string) {
	t.Helper()
	dir := t.TempDir()

	bin = filepath.Join(dir, "piper-stub")
	// args: --model <voice> --output_file <wav>; stdin is the text.
	piperScript := "#!/bin/sh\ncat > /dev/null\ntouch \"$4\"\n"
	if err := os.WriteFile(bin, []byte(piperScript), 0o700); err != nil {
		t.Fatalf("writing piper stub: %v", err)
	}

	playedMarker = filepath.Join(dir, "played")
	play = filepath.Join(dir, "play-stub")
	playScript := "#!/bin/sh\necho \"$1\" > " + playedMarker + "\n"
	if err := os.WriteFile(play, []byte(playScript), 0o700); err != nil {
		t.Fatalf("writing play stub: %v", err)
	}

	voice = filepath.Join(dir, "voice.onnx")
	if err := os.WriteFile(voice, []byte("fake"), 0o600); err != nil {
		t.Fatalf("writing voice: %v", err)
	}
	return bin, play, voice, playedMarker
}

func TestSpeakRendersAndPlays(t *testing.T) {
	bin, play, voice, played := stubSpeech(t)
	if err := New(bin, play, voice).Speak(context.Background(), "hello there"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	if _, err := os.Stat(played); err != nil {
		t.Fatal("playback was never invoked")
	}
}

func TestSpeakEmptyTextIsSilentNoOp(t *testing.T) {
	bin, play, voice, played := stubSpeech(t)
	if err := New(bin, play, voice).Speak(context.Background(), "   "); err != nil {
		t.Fatalf("Speak(empty): %v", err)
	}
	if _, err := os.Stat(played); err == nil {
		t.Fatal("empty text must not reach playback")
	}
}

func TestSpeakMissingVoiceHints(t *testing.T) {
	bin, play, _, _ := stubSpeech(t)
	c := New(bin, play, filepath.Join(t.TempDir(), "nope.onnx"))
	if err := c.Speak(context.Background(), "x"); err == nil {
		t.Fatal("missing voice model must error with a hint")
	}
}

func TestSpeakMissingBinaryHints(t *testing.T) {
	_, play, voice, _ := stubSpeech(t)
	c := New("definitely-not-installed-tts", play, voice)
	if err := c.Speak(context.Background(), "x"); err == nil {
		t.Fatal("missing piper must error with an install hint")
	}
}
