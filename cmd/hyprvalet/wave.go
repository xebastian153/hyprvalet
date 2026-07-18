package main

// The console wave: purely cosmetic, purely frontend. A calm flat line while
// the assistant listens; a moving waveform while it speaks. Hand-rolled with
// block glyphs and ANSI — no TUI framework, no dependencies — and it draws
// only on a real terminal, so piped output stays clean.

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/xebastian153/hyprvalet/internal/adapters/speech"
)

var waveGlyphs = []rune("▁▂▃▄▅▆▇█")

const waveWidth = 44

// stdoutIsTTY reports whether stdout is an interactive terminal — animation
// belongs on screens, never in pipes.
func stdoutIsTTY() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// listeningWave prints the calm line shown while recording: flat and still —
// the assistant is all ears. Returns a func that clears it.
func listeningWave() func() {
	if !stdoutIsTTY() {
		return func() {}
	}
	fmt.Print("\x1b[2m" + strings.Repeat("▁", waveWidth) + "\x1b[0m")
	return func() { fmt.Print("\r\x1b[K") }
}

// wave is the animated speaking indicator.
type wave struct {
	stop chan struct{}
	done chan struct{}
}

// startWave begins animating the waveform on the current line; halt stops it
// and cleans the line. A nil wave (non-TTY) is a silent no-op.
func startWave() *wave {
	if !stdoutIsTTY() {
		return nil
	}
	w := &wave{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		fmt.Print("\x1b[?25l") // hide the cursor while animating
		defer fmt.Print("\r\x1b[K\x1b[?25h")

		tick := time.NewTicker(60 * time.Millisecond)
		defer tick.Stop()
		phase := 0.0
		for {
			select {
			case <-w.stop:
				return
			case <-tick.C:
				var b strings.Builder
				for i := 0; i < waveWidth; i++ {
					// A traveling sine with per-column jitter reads as a voice,
					// not a metronome.
					v := (math.Sin(phase+float64(i)*0.35) + 1) / 2
					v *= 0.35 + 0.65*rand.Float64()
					b.WriteRune(waveGlyphs[int(v*float64(len(waveGlyphs)-1))])
				}
				fmt.Print("\r\x1b[36m" + b.String() + "\x1b[0m")
				phase += 0.5
			}
		}
	}()
	return w
}

func (w *wave) halt() {
	if w == nil {
		return
	}
	close(w.stop)
	<-w.done
}

// animated decorates a Speaker so the console waves exactly while the
// assistant's voice plays — Speak blocks for the duration of playback, which
// is what makes the animation honest.
type animated struct {
	inner speech.Speaker
}

func (a animated) Speak(ctx context.Context, text string) error {
	w := startWave()
	err := a.inner.Speak(ctx, text)
	w.halt()
	return err
}
