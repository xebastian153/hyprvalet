package mic

import (
	"math"
	"sort"
)

// Voice activity detection by frame energy: speech raises the RMS of the
// signal above the ambient floor; sustained silence after it marks the end of
// an utterance. The detector is a pure state machine over per-frame RMS values
// — no audio I/O — so the whole hands-free behavior is testable with numbers.

// vadEvent is what one fed frame means for the utterance state.
type vadEvent int

const (
	vadNone  vadEvent = iota // nothing changed
	vadStart                 // an utterance just began (speech sustained)
	vadEnd                   // the utterance just ended (silence sustained, or cap hit)
)

// vadConfig tunes the detector, all in frames (the caller decides frame size).
type vadConfig struct {
	// triggerFrames of consecutive voice before an utterance starts — brief
	// pops and clicks stay ignored.
	triggerFrames int
	// endSilence of consecutive silence that closes an utterance — the pause
	// that means "I finished talking", not a breath between words.
	endSilence int
	// maxFrames caps an utterance so a noisy room cannot buffer forever.
	maxFrames int
}

// vad is the utterance state machine.
type vad struct {
	cfg       vadConfig
	threshold float64
	voiced    int
	silent    int
	speaking  bool
	length    int
}

func newVAD(cfg vadConfig, threshold float64) *vad {
	return &vad{cfg: cfg, threshold: threshold}
}

// feed ingests one frame's RMS and reports what changed.
func (v *vad) feed(rms float64) vadEvent {
	voiced := rms >= v.threshold

	if !v.speaking {
		if voiced {
			v.voiced++
			if v.voiced >= v.cfg.triggerFrames {
				v.speaking = true
				v.silent = 0
				v.length = v.voiced
				return vadStart
			}
		} else {
			v.voiced = 0
		}
		return vadNone
	}

	v.length++
	if v.length >= v.cfg.maxFrames {
		v.reset()
		return vadEnd
	}
	if voiced {
		v.silent = 0
		return vadNone
	}
	v.silent++
	if v.silent >= v.cfg.endSilence {
		v.reset()
		return vadEnd
	}
	return vadNone
}

func (v *vad) reset() {
	v.speaking = false
	v.voiced = 0
	v.silent = 0
	v.length = 0
}

// ring is a fixed-size circular buffer of frame energies whose minimum is the
// live noise floor. Keeping the last ~1s means a passing noise ages out and the
// floor returns to the room's true quiet.
type ring struct {
	buf  []float64
	i    int
	full bool
}

func newRing(n int) *ring { return &ring{buf: make([]float64, n)} }

func (r *ring) push(v float64) {
	r.buf[r.i] = v
	r.i = (r.i + 1) % len(r.buf)
	if r.i == 0 {
		r.full = true
	}
}

// min returns the smallest value currently held (the noise floor).
func (r *ring) min() float64 {
	n := r.i
	if r.full {
		n = len(r.buf)
	}
	if n == 0 {
		return 0
	}
	m := r.buf[0]
	for j := 1; j < n; j++ {
		if r.buf[j] < m {
			m = r.buf[j]
		}
	}
	return m
}

// percentile returns the value at fraction p (0..1) of the sorted samples — a
// noise-floor estimate robust to speech mixed into the calibration window.
func percentile(samples []float64, p float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	sorted := append([]float64(nil), samples...)
	sort.Float64s(sorted)
	i := int(p * float64(len(sorted)-1))
	return sorted[i]
}

// frameRMS computes the root-mean-square energy of one s16le PCM frame.
func frameRMS(frame []byte) float64 {
	if len(frame) < 2 {
		return 0
	}
	var sum float64
	n := len(frame) / 2
	for i := 0; i < n; i++ {
		s := float64(int16(uint16(frame[2*i]) | uint16(frame[2*i+1])<<8))
		sum += s * s
	}
	return math.Sqrt(sum / float64(n))
}
