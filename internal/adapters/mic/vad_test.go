package mic

import "testing"

func testVAD() *vad {
	return newVAD(vadConfig{triggerFrames: 3, endSilence: 5, maxFrames: 20}, 100)
}

// feedAll feeds a sequence of RMS values and returns the events that fired.
func feedAll(v *vad, values []float64) []vadEvent {
	var events []vadEvent
	for _, rms := range values {
		if e := v.feed(rms); e != vadNone {
			events = append(events, e)
		}
	}
	return events
}

func TestVADUtteranceLifecycle(t *testing.T) {
	v := testVAD()
	// silence, sustained voice (trigger), more voice, sustained silence (end)
	seq := []float64{10, 10, 200, 200, 200, 300, 10, 10, 10, 10, 10}
	events := feedAll(v, seq)
	if len(events) != 2 || events[0] != vadStart || events[1] != vadEnd {
		t.Fatalf("events = %v, want [start end]", events)
	}
}

func TestVADIgnoresBriefPops(t *testing.T) {
	v := testVAD()
	// two-frame pops never reach the three-frame trigger
	seq := []float64{10, 500, 500, 10, 10, 500, 10, 500, 500, 10}
	if events := feedAll(v, seq); len(events) != 0 {
		t.Fatalf("events = %v, want none — pops and clicks must not trigger", events)
	}
}

func TestVADBreathsDoNotEndUtterance(t *testing.T) {
	v := testVAD()
	// voice, a 3-frame pause (below the 5-frame end), voice again, then real silence
	seq := []float64{200, 200, 200, 10, 10, 10, 200, 200, 10, 10, 10, 10, 10}
	events := feedAll(v, seq)
	if len(events) != 2 || events[0] != vadStart || events[1] != vadEnd {
		t.Fatalf("events = %v, want one utterance spanning the breath", events)
	}
}

func TestVADCapsRunawayUtterance(t *testing.T) {
	v := testVAD()
	var seq []float64
	for i := 0; i < 30; i++ {
		seq = append(seq, 300) // a noisy room that never goes quiet
	}
	events := feedAll(v, seq)
	// The cap must force an end; sustained noise may then legitimately start a
	// new utterance — the point is that no utterance grows without bound.
	if len(events) < 2 || events[0] != vadStart || events[1] != vadEnd {
		t.Fatalf("events = %v, want the cap to force an end", events)
	}
}

func TestFrameRMS(t *testing.T) {
	silence := make([]byte, 32)
	if rms := frameRMS(silence); rms != 0 {
		t.Fatalf("silence RMS = %v, want 0", rms)
	}
	// A constant amplitude of 1000 has an RMS of 1000.
	loud := make([]byte, 32)
	for i := 0; i < len(loud); i += 2 {
		loud[i] = byte(1000 & 0xff)
		loud[i+1] = byte(1000 >> 8)
	}
	if rms := frameRMS(loud); rms < 999 || rms > 1001 {
		t.Fatalf("constant-1000 RMS = %v, want ~1000", rms)
	}
}

func TestPercentileRobustToSpeech(t *testing.T) {
	// A quiet floor of ~100 with loud speech spikes mixed in — the low
	// percentile must recover the floor, not the mean (which the speech skews).
	samples := []float64{95, 100, 105, 98, 3000, 2800, 3200, 102, 99, 101}
	if got := percentile(samples, 0.25); got < 90 || got > 110 {
		t.Fatalf("percentile(0.25) = %.0f, want the ~100 floor, not the speech-skewed mean", got)
	}
	if percentile(nil, 0.25) != 0 {
		t.Fatal("empty samples must be 0")
	}
}
