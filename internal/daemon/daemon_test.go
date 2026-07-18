package daemon

import (
	"context"
	"io"
	"log"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/xebastian153/hyprvalet/internal/core"
	"github.com/xebastian153/hyprvalet/internal/protocol"
)

// demoCap is a harmless capability for daemon tests: its Run records that it ran
// and returns a canned string, touching no real desktop.
type demoCap struct {
	id  string
	ran *bool
}

func (c demoCap) ID() string            { return c.id }
func (demoCap) Description() string     { return "demo" }
func (demoCap) Access() core.AccessKind { return core.AccessWorkspace }
func (demoCap) Risk() core.Risk         { return core.RiskSafe }
func (demoCap) Params() []string        { return nil }
func (c demoCap) Run(context.Context, core.Args) (string, error) {
	if c.ran != nil {
		*c.ran = true
	}
	return "did it", nil
}

func testDaemon(t *testing.T, rules core.PolicyRules, caps ...core.Capability) *Daemon {
	t.Helper()
	reg := core.NewRegistry()
	for _, c := range caps {
		if err := reg.Register(c); err != nil {
			t.Fatalf("registering: %v", err)
		}
	}
	return &Daemon{
		reg:     reg,
		rules:   rules,
		arm:     core.ArmState{},
		session: core.SessionAllow{},
		mailbox: make(chan command),
		log:     log.New(io.Discard, "", 0),
	}
}

func TestHandlePingAndList(t *testing.T) {
	d := testDaemon(t, core.PolicyRules{}, demoCap{id: "a.b"})

	if resp := d.handle(protocol.Request{Op: protocol.OpPing}); resp.Status != protocol.StatusPong || resp.Count != 1 {
		t.Fatalf("ping = %+v", resp)
	}
	resp := d.handle(protocol.Request{Op: protocol.OpList})
	if resp.Status != protocol.StatusCaps || len(resp.Caps) != 1 || resp.Caps[0].ID != "a.b" {
		t.Fatalf("list = %+v", resp)
	}
}

func TestHandleRun(t *testing.T) {
	rule := func(d core.Decision) core.PolicyRules {
		return core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: d}}}
	}
	tests := []struct {
		name    string
		rules   core.PolicyRules
		cap     string
		want    protocol.Status
		wantRan bool
	}{
		{"allow runs it", rule(core.DecisionAllow), "a.b", protocol.StatusRan, true},
		{"deny refuses without running", rule(core.DecisionDeny), "a.b", protocol.StatusDenied, false},
		{"ask needs confirmation, never auto-runs", rule(core.DecisionAsk), "a.b", protocol.StatusNeedsConfirm, false},
		{"unknown capability errors", rule(core.DecisionAllow), "nope", protocol.StatusError, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran := false
			d := testDaemon(t, tt.rules, demoCap{id: "a.b", ran: &ran})
			resp := d.handle(protocol.Request{Op: protocol.OpRun, Cap: tt.cap})
			if resp.Status != tt.want {
				t.Fatalf("status = %q, want %q (%+v)", resp.Status, tt.want, resp)
			}
			if ran != tt.wantRan {
				t.Fatalf("ran = %v, want %v", ran, tt.wantRan)
			}
		})
	}
}

func TestDoomLoopRefusesToAutoRun(t *testing.T) {
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	ran := false
	d := testDaemon(t, allow, demoCap{id: "a.b", ran: &ran})
	sig := core.ActionSignature("a.b", nil)
	now := time.Now()
	for i := 0; i < core.DoomLoopThreshold-1; i++ {
		d.history = append(d.history, core.ActionRecord{Signature: sig, At: now})
	}
	resp := d.handle(protocol.Request{Op: protocol.OpRun, Cap: "a.b"})
	if resp.Status != protocol.StatusNeedsConfirm || ran {
		t.Fatalf("a resident daemon must not auto-run a loop: %+v ran=%v", resp, ran)
	}
}

func TestHandleRunApproved(t *testing.T) {
	rule := func(d core.Decision) core.PolicyRules {
		return core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: d}}}
	}

	t.Run("approval lets an Ask run", func(t *testing.T) {
		ran := false
		d := testDaemon(t, rule(core.DecisionAsk), demoCap{id: "a.b", ran: &ran})
		resp := d.handle(protocol.Request{Op: protocol.OpRun, Cap: "a.b", Approved: true})
		if resp.Status != protocol.StatusRan || !ran {
			t.Fatalf("approved ask = %+v ran=%v", resp, ran)
		}
	})

	t.Run("approval never overrides a Deny", func(t *testing.T) {
		ran := false
		d := testDaemon(t, rule(core.DecisionDeny), demoCap{id: "a.b", ran: &ran})
		resp := d.handle(protocol.Request{Op: protocol.OpRun, Cap: "a.b", Approved: true})
		if resp.Status != protocol.StatusDenied || ran {
			t.Fatalf("approved deny = %+v ran=%v (approval must not widen policy)", resp, ran)
		}
	})

	t.Run("approval runs through a doom-loop", func(t *testing.T) {
		ran := false
		d := testDaemon(t, rule(core.DecisionAllow), demoCap{id: "a.b", ran: &ran})
		sig := core.ActionSignature("a.b", nil)
		now := time.Now()
		for i := 0; i < core.DoomLoopThreshold-1; i++ {
			d.history = append(d.history, core.ActionRecord{Signature: sig, At: now})
		}
		resp := d.handle(protocol.Request{Op: protocol.OpRun, Cap: "a.b", Approved: true})
		if resp.Status != protocol.StatusRan || !ran {
			t.Fatalf("approved doom-loop = %+v ran=%v", resp, ran)
		}
	})
}

// serveDaemon starts a daemon on a fresh temp socket and returns its path. The
// daemon and its socket are torn down when the test ends.
func serveDaemon(t *testing.T, rules core.PolicyRules, ran *bool) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	d := testDaemon(t, rules, demoCap{id: "a.b", ran: ran})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); ln.Close() })
	go func() { _ = d.Run(ctx, ln) }()
	return socket
}

func TestRunViaDaemonConfirmation(t *testing.T) {
	askRule := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAsk}}}

	t.Run("yes runs it over the socket", func(t *testing.T) {
		ran := false
		socket := serveDaemon(t, askRule, &ran)
		asked := false
		resp, err := RunViaDaemon(socket, "a.b", nil, func(string) bool { asked = true; return true })
		if err != nil {
			t.Fatalf("RunViaDaemon: %v", err)
		}
		if !asked || resp.Status != protocol.StatusRan || !ran {
			t.Fatalf("asked=%v resp=%+v ran=%v", asked, resp, ran)
		}
	})

	t.Run("no declines without running", func(t *testing.T) {
		ran := false
		socket := serveDaemon(t, askRule, &ran)
		resp, err := RunViaDaemon(socket, "a.b", nil, func(string) bool { return false })
		if err != nil {
			t.Fatalf("RunViaDaemon: %v", err)
		}
		if resp.Status != protocol.StatusDenied || ran {
			t.Fatalf("declined: resp=%+v ran=%v", resp, ran)
		}
	})
}

func TestSocketRoundTrip(t *testing.T) {
	socket := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	d := testDaemon(t, core.PolicyRules{}, demoCap{id: "a.b"})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx, ln) }()

	resp, err := Send(socket, protocol.Request{Op: protocol.OpPing})
	if err != nil {
		t.Fatalf("Send over socket: %v", err)
	}
	if resp.Status != protocol.StatusPong || resp.Count != 1 {
		t.Fatalf("pong over socket = %+v", resp)
	}
	cancel()
	_ = ln.Close()
}
