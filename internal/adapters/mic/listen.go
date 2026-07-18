package mic

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"time"
)

// ErrIdle is returned by ListenOnce when no speech begins within the idle
// window — the signal a conversation uses to close itself after a quiet spell.
var ErrIdle = errors.New("no speech within the idle window")

// Hands-free capture parameters. Frames are 30ms of 16kHz mono s16 audio.
const (
	sampleRate = 16000
	frameMs    = 30
	frameBytes = sampleRate * frameMs / 1000 * 2 // 960

	calibrateFrames = 25  // ~750ms of ambient to learn the noise floor
	preRollFrames   = 10  // ~300ms kept from before the trigger — no clipped first syllable
	triggerFrames   = 3   // ~90ms of sustained voice starts an utterance
	endSilenceOf    = 43  // ~1.3s of silence ends it — a thinking pause mid-dictation is longer than a breath
	maxUtterance    = 660 // ~20s cap

	// thresholdFactor scales the ambient floor into a voice threshold;
	// thresholdMin keeps a very quiet room from triggering on anything;
	// loudAmbient is the floor above which we warn that speech may not be
	// heard over the room.
	thresholdFactor = 3.5
	thresholdMin    = 260
	loudAmbient     = 1200

	// stallTimeout: pw-record delivers a frame every ~30ms; three seconds of
	// nothing means the recorder has wedged, and the turn must end rather than
	// hang the whole session.
	stallTimeout = 3 * time.Second
)

// ListenOnce blocks until one spoken utterance is captured, then writes it as
// a 16kHz mono WAV at wavPath. Hands-free: speech starts the capture, a pause
// ends it. The ambient noise floor is measured at the start of every call, so
// the threshold adapts to the room as it is right now. Returns ctx.Err() when
// cancelled. If idle > 0 and no speech begins within it, returns ErrIdle —
// once an utterance starts, idle no longer applies; it always finishes.
func ListenOnce(ctx context.Context, wavPath string, idle time.Duration) error {
	args := []string{"--rate", "16000", "--channels", "1", "--format", "s16"}
	if target := defaultSource(); target != "" {
		args = append(args, "--target", target)
	}
	args = append(args, "-")

	cmd := exec.CommandContext(ctx, "pw-record", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting recording: %w", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// pw-record prefixes stdout with a 24-byte AU header; PCM follows.
	if _, err := io.ReadFull(stdout, make([]byte, 24)); err != nil {
		return fmt.Errorf("reading stream header: %w", err)
	}

	// Read frames in a goroutine feeding a channel, so a stalled recorder — one
	// that stops delivering bytes without closing the pipe — cannot block the
	// loop forever the way a bare io.ReadFull would. The loop selects the next
	// frame against the context, the idle deadline, AND a stall watchdog.
	frames := make(chan []byte, 4)
	readErr := make(chan error, 1)
	go func() {
		for {
			buf := make([]byte, frameBytes)
			if _, err := io.ReadFull(stdout, buf); err != nil {
				readErr <- err
				return
			}
			select {
			case frames <- buf:
			case <-ctx.Done():
				return
			}
		}
	}()

	frame := make([]byte, frameBytes)
	readFrame := func() error {
		select {
		case buf := <-frames:
			copy(frame, buf)
			return nil
		case err := <-readErr:
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(stallTimeout):
			return fmt.Errorf("the microphone stopped delivering audio")
		}
	}

	// Calibrate: learn the room before listening for a voice.
	var ambient float64
	for i := 0; i < calibrateFrames; i++ {
		if err := readFrame(); err != nil {
			return err
		}
		ambient += frameRMS(frame)
	}
	ambient /= calibrateFrames
	threshold := math.Max(ambient*thresholdFactor, thresholdMin)
	if ambient > loudAmbient {
		// A loud room (music playing) raises the threshold so far that speech
		// may never cross it — the assistant goes silently deaf. Deafness must
		// at least be VISIBLE until echo cancellation lands.
		fmt.Fprintf(os.Stderr, "warning: ambient noise is high (RMS %.0f) — I may not hear you over it\n", ambient)
	}

	det := newVAD(vadConfig{
		triggerFrames: triggerFrames,
		endSilence:    endSilenceOf,
		maxFrames:     maxUtterance,
	}, threshold)

	var preRoll [][]byte
	var utterance []byte
	var elapsed time.Duration
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := readFrame(); err != nil {
			return err
		}
		// Idle only governs the silence BEFORE speech begins; once an utterance
		// is underway it always finishes. Time is counted in frames read, so it
		// needs no clock.
		if idle > 0 && len(utterance) == 0 {
			elapsed += frameMs * time.Millisecond
			if elapsed >= idle {
				return ErrIdle
			}
		}

		switch det.feed(frameRMS(frame)) {
		case vadStart:
			// The trigger frames themselves live in the pre-roll; keep it all.
			for _, f := range preRoll {
				utterance = append(utterance, f...)
			}
			preRoll = nil
			utterance = append(utterance, frame...)
		case vadEnd:
			return writeWAV(wavPath, utterance)
		default:
			if len(utterance) > 0 {
				utterance = append(utterance, frame...)
				continue
			}
			cp := make([]byte, len(frame))
			copy(cp, frame)
			preRoll = append(preRoll, cp)
			if len(preRoll) > preRollFrames {
				preRoll = preRoll[1:]
			}
		}
	}
}

// writeWAV wraps raw 16kHz mono s16le PCM in a standard WAV container.
func writeWAV(path string, pcm []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("writing wav: %w", err)
	}
	defer f.Close()

	var h [44]byte
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+len(pcm)))
	copy(h[8:], "WAVE")
	copy(h[12:], "fmt ")
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1) // PCM
	binary.LittleEndian.PutUint16(h[22:], 1) // mono
	binary.LittleEndian.PutUint32(h[24:], sampleRate)
	binary.LittleEndian.PutUint32(h[28:], sampleRate*2) // byte rate
	binary.LittleEndian.PutUint16(h[32:], 2)            // block align
	binary.LittleEndian.PutUint16(h[34:], 16)           // bits per sample
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(len(pcm)))

	if _, err := f.Write(h[:]); err != nil {
		return fmt.Errorf("writing wav: %w", err)
	}
	if _, err := f.Write(pcm); err != nil {
		return fmt.Errorf("writing wav: %w", err)
	}
	return nil
}
