package daemon

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
	"github.com/xebastian153/hyprvalet/internal/protocol"
)

type slowCap struct {
	id    string
	sleep time.Duration
	ran   *bool
}

func (c slowCap) ID() string            { return c.id }
func (slowCap) Description() string     { return "slow" }
func (slowCap) Access() core.AccessKind { return core.AccessWorkspace }
func (slowCap) Risk() core.Risk         { return core.RiskSafe }
func (slowCap) Params() []string        { return nil }
func (c slowCap) Run(ctx context.Context, _ core.Args) (string, error) {
	select {
	case <-time.After(c.sleep):
		if c.ran != nil {
			*c.ran = true
		}
		return "slow ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func TestRealtime_CancelAbortsInFlightRun(t *testing.T) {
	ran := false
	d := testRealtimeDaemon(t, core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}, slowCap{id: "a.b", sleep: 5 * time.Second, ran: &ran})
	d.StartRealtimeSession()
	d.historyPath = filepath.Join(t.TempDir(), "actions.json")
	gen := d.RealtimeGeneration()
	start := time.Now()
	done := make(chan protocol.Response, 1)
	go func() { done <- d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: gen}) }()
	time.Sleep(10 * time.Millisecond)
	d.RealtimeCancel()
	select {
	case resp := <-done:
		if time.Since(start) > 500*time.Millisecond {
			t.Fatalf("handle took %v, want <500ms after cancel, resp=%+v", time.Since(start), resp)
		}
		if resp.Status != protocol.StatusCancelled {
			t.Fatalf("want cancelled, got %+v", resp)
		}
		if ran || len(d.history) != 0 {
			t.Fatalf("cancelled must not run or persist: ran=%v hist=%d", ran, len(d.history))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("handle did not abort within 500ms — context not bound to RealtimeCancel")
	}
}
