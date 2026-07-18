package main

// The console presence: purely cosmetic, purely frontend, TTY-only. A header
// logo, a status line that names what the assistant is doing right now
// (listening / thinking / speaking), and a waveform that is flat while it
// listens and alive while it speaks. Hand-rolled with block glyphs and ANSI —
// no TUI framework, no dependencies — so piped output stays clean.

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

const waveWidth = 46

// logo is the standing banner of the conversation window.
const logo = "\x1b[36m" + `
   ┏  ┏━┓ ┏━┓ ┓ ┏ ┳ ┏━┓
   ┃  ┣━┫ ┣┳┛ ┃┏┛ ┃ ┗━┓
  ┗┛  ┛ ┗ ┛┗  ┗┛  ┻ ┗━┛   hyprvalet` + "\x1b[0m"

// stdoutIsTTY reports whether stdout is an interactive terminal — animation
// belongs on screens, never in pipes.
func stdoutIsTTY() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// banner prints the logo and a one-line hint once, at the top of a session.
func banner(hint string) {
	if !stdoutIsTTY() {
		return
	}
	fmt.Println(logo)
	fmt.Printf("\x1b[2m  %s\x1b[0m\n\n", hint)
}

// status prints a labeled state line the user can read at a glance: a colored
// dot, the state word, and a trailing detail. It overwrites its own line.
func status(state, detail string) {
	if !stdoutIsTTY() {
		if detail != "" {
			fmt.Println(detail)
		}
		return
	}
	dot := map[string]string{
		"listening": "\x1b[32m●\x1b[0m", // green: your turn
		"thinking":  "\x1b[33m◐\x1b[0m", // amber: working
		"speaking":  "\x1b[36m◆\x1b[0m", // cyan: my turn
	}[state]
	fmt.Printf("\r\x1b[K%s \x1b[1m%s\x1b[0m  \x1b[2m%s\x1b[0m\n", dot, state, detail)
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
