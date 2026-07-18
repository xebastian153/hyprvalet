package mic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestListenOnceLive exercises the real capture pipeline. It needs a real
// PipeWire source, so it only runs when explicitly asked for:
//
//	HYPRVALET_LIVE_MIC=1 go test ./internal/adapters/mic -run Live
func TestListenOnceLive(t *testing.T) {
	if os.Getenv("HYPRVALET_LIVE_MIC") == "" {
		t.Skip("set HYPRVALET_LIVE_MIC=1 to run against the real microphone")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// In a quiet room nothing triggers, so cancellation is the expected exit —
	// proving the capture loop starts, streams, and honors the context.
	err := ListenOnce(ctx, filepath.Join(t.TempDir(), "u.wav"), Params{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("quiet-room ListenOnce = %v, want deadline exceeded", err)
	}
}
