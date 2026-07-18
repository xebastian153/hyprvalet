// Package mic captures speech from the default PipeWire source into a WAV file
// whisper.cpp can read. Like the whisper adapter it lives entirely at the
// frontend edge: the rest of hyprvalet never knows audio existed.
package mic

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Start begins recording the default input to wavPath in the format whisper.cpp
// expects (16 kHz, mono, s16). It returns a stop function that ends the
// recording and finalizes the file. Recording is push-to-talk shaped: the
// caller decides when speech ends (a keypress today, silence detection later).
func Start(wavPath string) (stop func() error, err error) {
	return start("pw-record", defaultSource(), wavPath)
}

// defaultSource resolves which input node the recording stream targets.
// HYPRVALET_MIC_TARGET pins it explicitly — the echo-cancelled source, for
// instance, so the assistant hears you without hearing itself, while the rest
// of the system keeps its own default. Otherwise it falls back to the system
// default (named explicitly, because on some PipeWire setups an untargeted
// capture fails with "no target node available"). Empty means let pw-record
// choose.
func defaultSource() string {
	if t := strings.TrimSpace(os.Getenv("HYPRVALET_MIC_TARGET")); t != "" {
		return t
	}
	out, err := exec.Command("pactl", "get-default-source").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func start(bin, target, wavPath string) (func() error, error) {
	args := []string{"--rate", "16000", "--channels", "1", "--format", "s16"}
	if target != "" {
		args = append(args, "--target", target)
	}
	args = append(args, wavPath)
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			return nil, fmt.Errorf("%s not found — is PipeWire installed?", bin)
		}
		return nil, fmt.Errorf("starting recording: %w", err)
	}
	return func() error {
		// SIGINT lets pw-record finalize the WAV header; a hard kill would leave
		// a truncated file whisper cannot parse.
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			return fmt.Errorf("stopping recording: %w", err)
		}
		// The interrupt is the expected exit path, not a failure — but a
		// recorder that died at startup (a transient audio-server refusal)
		// exits the same way. The file is the truth: no audio beyond a WAV
		// header means nothing was recorded, and the caller must know rather
		// than transcribe silence — or worse, a stale previous recording.
		if err := cmd.Wait(); err != nil {
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) {
				return fmt.Errorf("finishing recording: %w", err)
			}
		}
		if info, err := os.Stat(wavPath); err != nil || info.Size() <= 44 {
			return fmt.Errorf("recording produced no audio — is the microphone available?")
		}
		return nil
	}, nil
}
