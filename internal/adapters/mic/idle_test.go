package mic

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestListenOnceIdle proves a silent room returns ErrIdle within the idle
// window rather than blocking forever. Live (real mic), guarded like the
// sibling test.
func TestListenOnceIdle(t *testing.T) {
	if os.Getenv("HYPRVALET_LIVE_MIC") == "" {
		t.Skip("set HYPRVALET_LIVE_MIC=1 to run against the real microphone")
	}
	// Generous ctx; the idle window (1s) must fire first.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := ListenOnce(ctx, filepath.Join(t.TempDir(), "u.wav"), time.Second)
	if !errors.Is(err, ErrIdle) {
		t.Fatalf("silent-room ListenOnce(idle=1s) = %v, want ErrIdle", err)
	}
}
