package realtime

import (
	"errors"
)

// MaxBatchBytes is the maximum PCM batch size for streaming (6400 bytes).
// Corresponds to task 1.4 requirement: buffering ≤6400B/batch.
const MaxBatchBytes = 6400

// MaxBatchSamples is MaxBatchBytes divided by 2 (16-bit samples).
const MaxBatchSamples = MaxBatchBytes / 2 // 3200

// ResamplePCM resamples s16 PCM from fromRate to toRate using linear
// interpolation. Deterministic and pure (no side effects). Returns error on
// invalid rates. Used for 24k Qwen3-TTS output → PipeWire sink and 16k input
// handling without blocking capture.
//
// Example: 480 samples at 24k → 320 at 16k (ratio 2/3), 160 at 16k → 240 at
// 24k (ratio 3/2). Same rate is passthrough (copied).
func ResamplePCM(in []int16, fromRate, toRate int) ([]int16, error) {
	if fromRate <= 0 || toRate <= 0 {
		return nil, errors.New("resample: rates must be > 0")
	}
	if len(in) == 0 {
		return []int16{}, nil
	}
	if fromRate == toRate {
		out := make([]int16, len(in))
		copy(out, in)
		return out, nil
	}
	// Output length rounded to nearest sample.
	outLen := int(float64(len(in))*float64(toRate)/float64(fromRate) + 0.5)
	if outLen == 0 {
		outLen = 1
	}
	out := make([]int16, outLen)
	ratio := float64(fromRate) / float64(toRate)
	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * ratio
		idx := int(srcPos)
		frac := srcPos - float64(idx)
		if idx >= len(in)-1 {
			// clamp to last sample
			out[i] = in[len(in)-1]
			continue
		}
		if idx < 0 {
			out[i] = in[0]
			continue
		}
		// linear interpolate between in[idx] and in[idx+1]
		a := float64(in[idx])
		b := float64(in[idx+1])
		v := a*(1-frac) + b*frac
		// clamp to int16
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out[i] = int16(v)
	}
	return out, nil
}

// ChunkPCM splits pcm into chunks of at most chunkSamples samples each,
// enforcing MaxBatchBytes / MaxBatchSamples limit. Preserves order and total
// sample count. If chunkSamples <=0, defaults to MaxBatchSamples.
func ChunkPCM(pcm []int16, chunkSamples int) [][]int16 {
	if chunkSamples <= 0 {
		chunkSamples = MaxBatchSamples
	}
	if chunkSamples > MaxBatchSamples {
		chunkSamples = MaxBatchSamples
	}
	if len(pcm) == 0 {
		return nil
	}
	var chunks [][]int16
	for len(pcm) > 0 {
		n := chunkSamples
		if n > len(pcm) {
			n = len(pcm)
		}
		chunks = append(chunks, pcm[:n])
		pcm = pcm[n:]
	}
	return chunks
}
