package edgetts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stubs(t *testing.T) (bin, play, argsFile, marker string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	bin = filepath.Join(dir, "edge-stub")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho \"$@\" > "+argsFile+"\n"), 0o700); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	marker = filepath.Join(dir, "played")
	play = filepath.Join(dir, "play-stub")
	if err := os.WriteFile(play, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o700); err != nil {
		t.Fatalf("writing stub: %v", err)
	}
	return
}

func TestSpeakRunsCLIAndPlays(t *testing.T) {
	bin, play, argsFile, marker := stubs(t)
	if err := New(bin, play, "es-AR-TomasNeural").Speak(context.Background(), "hola"); err != nil {
		t.Fatalf("Speak: %v", err)
	}
	args, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(args), "es-AR-TomasNeural") || !strings.Contains(string(args), "hola") {
		t.Fatalf("cli args = %q", args)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("playback was never invoked")
	}
}

func TestSpeakMissingBinaryHints(t *testing.T) {
	_, play, _, _ := stubs(t)
	if err := New("definitely-not-installed-edge", play, "v").Speak(context.Background(), "x"); err == nil {
		t.Fatal("missing binary must error (so the chain falls through)")
	}
}
