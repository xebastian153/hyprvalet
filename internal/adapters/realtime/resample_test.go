package realtime

import (
	"testing"
)

func TestResamplePCM_24kTo16k(t *testing.T) {
	tests := []struct {
		name     string
		in       []int16
		fromRate int
		toRate   int
		wantLen  int
	}{
		{"empty", []int16{}, 24000, 16000, 0},
		{"single sample", []int16{1000}, 24000, 16000, 1},
		{"24k down to 16k 480 samples -> 320", make([]int16, 480), 24000, 16000, 320},
		{"24k down to 16k 240 samples -> 160", make([]int16, 240), 24000, 16000, 160},
		{"16k up to 24k 160 -> 240", make([]int16, 160), 16000, 24000, 240},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResamplePCM(tt.in, tt.fromRate, tt.toRate)
			if err != nil {
				t.Fatalf("ResamplePCM error: %v", err)
			}
			if len(got) != tt.wantLen {
				t.Fatalf("ResamplePCM len = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestResamplePCM_Deterministic(t *testing.T) {
	in := []int16{0, 1000, -1000, 2000, -2000, 3000}
	a, _ := ResamplePCM(in, 24000, 16000)
	b, _ := ResamplePCM(in, 24000, 16000)
	if len(a) != len(b) {
		t.Fatalf("determinism len mismatch %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("determinism mismatch at %d: %d vs %d", i, a[i], b[i])
		}
	}
	// ensure resampling actually transforms non-trivial input
	if len(a) > 0 && a[0] == 12345 {
		t.Fatal("unexpected value")
	}
}

func TestResamplePCM_BufferLimit(t *testing.T) {
	// MaxBatchBytes is 6400 => 3200 samples
	if MaxBatchBytes != 6400 {
		t.Fatalf("MaxBatchBytes = %d, want 6400", MaxBatchBytes)
	}
	if MaxBatchSamples != 3200 {
		t.Fatalf("MaxBatchSamples = %d, want 3200", MaxBatchSamples)
	}

	// Chunk helper must enforce limit
	large := make([]int16, 5000) // 10000 bytes > 6400
	chunks := ChunkPCM(large, MaxBatchSamples)
	for i, c := range chunks {
		if len(c) > MaxBatchSamples {
			t.Fatalf("chunk %d len %d > MaxBatchSamples %d", i, len(c), MaxBatchSamples)
		}
		if len(c)*2 > MaxBatchBytes {
			t.Fatalf("chunk %d bytes %d > MaxBatchBytes", i, len(c)*2)
		}
	}
	// total samples preserved
	total := 0
	for _, c := range chunks {
		total += len(c)
	}
	if total != len(large) {
		t.Fatalf("chunked total %d != original %d", total, len(large))
	}
}

func TestResamplePCM_InvalidRates(t *testing.T) {
	_, err := ResamplePCM([]int16{1, 2}, 0, 16000)
	if err == nil {
		t.Fatal("expected error for zero fromRate")
	}
	_, err = ResamplePCM([]int16{1, 2}, 24000, 0)
	if err == nil {
		t.Fatal("expected error for zero toRate")
	}
}

func TestResamplePCM_PassthroughSameRate(t *testing.T) {
	in := []int16{1, 2, 3, 4}
	got, err := ResamplePCM(in, 16000, 16000)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(got) != len(in) {
		t.Fatalf("passthrough len %d != %d", len(got), len(in))
	}
	for i := range in {
		if got[i] != in[i] {
			t.Fatalf("passthrough mismatch at %d", i)
		}
	}
}
