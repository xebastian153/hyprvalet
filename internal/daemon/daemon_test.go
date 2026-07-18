package daemon

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/xebastian153/hyprvalet/internal/adapters/policyfile"
	"github.com/xebastian153/hyprvalet/internal/core"
	"github.com/xebastian153/hyprvalet/internal/protocol"
)

// demoCap is a harmless capability for daemon tests: its Run records that it ran
// and returns a canned string (or a canned error), touching no real desktop.
type demoCap struct {
	id     string
	ran    *bool
	runErr error
}

func (c demoCap) ID() string            { return c.id }
func (demoCap) Description() string     { return "demo" }
func (demoCap) Access() core.AccessKind { return core.AccessWorkspace }
func (demoCap) Risk() core.Risk         { return core.RiskSafe }
func (demoCap) Params() []string        { return nil }
func (c demoCap) Run(context.Context, core.Args) (string, error) {
	if c.runErr != nil {
		return "", c.runErr
	}
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

// fakePlanner and fakeLLM are canned reasoning ports: they let the daemon's
// ask/plan path be tested end to end with no Ollama, no network, and a plan the
// test fully controls.
type fakePlanner struct {
	plan core.Plan
	err  error
}

func (f fakePlanner) Plan(context.Context, string, []core.Capability, []core.Event) (core.Plan, error) {
	return f.plan, f.err
}

type fakeLLM struct {
	intent core.Intent
	err    error
}

func (f fakeLLM) Interpret(context.Context, string, []core.Capability, []core.Event) (core.Intent, error) {
	return f.intent, f.err
}

// fakeEvents is an in-memory core.EventStore capturing what the daemon audits.
type fakeEvents struct {
	events []core.Event
}

func (f *fakeEvents) Append(e core.Event) error { f.events = append(f.events, e); return nil }
func (f *fakeEvents) Tail(int) ([]core.Event, error) {
	return f.events, nil
}

// runDaemon starts a daemon on a fresh temp socket and returns its path; the
// daemon and socket are torn down when the test ends.
func runDaemon(t *testing.T, d *Daemon) string {
	t.Helper()
	socket := filepath.Join(t.TempDir(), "d.sock")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); ln.Close() })
	go func() { _ = d.Run(ctx, ln) }()
	return socket
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

// serveDaemon starts a daemon serving one demo capability and returns its socket.
func serveDaemon(t *testing.T, rules core.PolicyRules, ran *bool) string {
	t.Helper()
	return runDaemon(t, testDaemon(t, rules, demoCap{id: "a.b", ran: ran}))
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

func TestReasonAskBindsSingleIntent(t *testing.T) {
	ran := false
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	d := testDaemon(t, allow, demoCap{id: "a.b", ran: &ran})
	d.llm = fakeLLM{intent: core.Intent{Capability: "a.b", Args: core.Args{"x": "1"}, Reasoning: "because"}}
	socket := runDaemon(t, d)

	resp, err := AskViaDaemon(socket, "do the thing")
	if err != nil {
		t.Fatalf("AskViaDaemon: %v", err)
	}
	if resp.Status != protocol.StatusPlanned || len(resp.Plan) != 1 {
		t.Fatalf("ask = %+v", resp)
	}
	step := resp.Plan[0]
	if step.Cap != "a.b" || step.Decision != "allow" || step.Args["x"] != "1" {
		t.Fatalf("bound step = %+v", step)
	}
	if resp.Reasoning != "because" {
		t.Fatalf("reasoning = %q", resp.Reasoning)
	}
	if ran {
		t.Fatal("ask must reason only — it must not run the capability")
	}
}

func TestReasonAskNoMatch(t *testing.T) {
	d := testDaemon(t, core.PolicyRules{}, demoCap{id: "a.b"})
	// The model chose a capability that is not registered: the allowlist check in
	// ResolveIntent turns that into an empty, harmless "no match", not a run.
	d.llm = fakeLLM{intent: core.Intent{Capability: "not.registered", Reasoning: "guessing"}}
	socket := runDaemon(t, d)

	resp, err := AskViaDaemon(socket, "do something impossible")
	if err != nil {
		t.Fatalf("AskViaDaemon: %v", err)
	}
	if resp.Status != protocol.StatusPlanned || len(resp.Plan) != 0 {
		t.Fatalf("expected planned with no steps, got %+v", resp)
	}
}

func TestReasonPlanBindsAndDoesNotRun(t *testing.T) {
	ran := false
	rules := core.PolicyRules{ByCapID: map[string]core.Rule{
		"a.b": {Decision: core.DecisionAllow},
		"c.d": {Decision: core.DecisionDeny},
	}}
	d := testDaemon(t, rules, demoCap{id: "a.b", ran: &ran}, demoCap{id: "c.d"})
	d.planner = fakePlanner{plan: core.Plan{
		Summary: "two steps",
		Steps:   []core.Step{{Capability: "a.b"}, {Capability: "c.d"}},
	}}
	socket := runDaemon(t, d)

	resp, err := PlanViaDaemon(socket, "go")
	if err != nil {
		t.Fatalf("PlanViaDaemon: %v", err)
	}
	if resp.Status != protocol.StatusPlanned || resp.Summary != "two steps" || len(resp.Plan) != 2 {
		t.Fatalf("plan = %+v", resp)
	}
	if resp.Plan[0].Decision != "allow" || resp.Plan[1].Decision != "deny" {
		t.Fatalf("decisions = %q, %q (want allow, deny)", resp.Plan[0].Decision, resp.Plan[1].Decision)
	}
	if ran {
		t.Fatal("plan must preview only — it must not run any step")
	}
}

func TestReasonPlanEmptyIsNotAnError(t *testing.T) {
	d := testDaemon(t, core.PolicyRules{})
	d.planner = fakePlanner{plan: core.Plan{Summary: "nothing to do"}}
	socket := runDaemon(t, d)

	resp, err := PlanViaDaemon(socket, "make coffee")
	if err != nil {
		t.Fatalf("PlanViaDaemon: %v", err)
	}
	if resp.Status != protocol.StatusPlanned || len(resp.Plan) != 0 {
		t.Fatalf("empty plan = %+v", resp)
	}
}

func TestHandleRunAuditsOutcomes(t *testing.T) {
	rule := func(d core.Decision) core.PolicyRules {
		return core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: d}}}
	}
	tests := []struct {
		name  string
		rules core.PolicyRules
		req   protocol.Request
		want  core.EventKind
	}{
		{"ran is audited", rule(core.DecisionAllow), protocol.Request{Op: protocol.OpRun, Cap: "a.b"}, core.EventRan},
		{"denial is audited", rule(core.DecisionDeny), protocol.Request{Op: protocol.OpRun, Cap: "a.b"}, core.EventDenied},
		{"needs_confirm is audited", rule(core.DecisionAsk), protocol.Request{Op: protocol.OpRun, Cap: "a.b"}, core.EventNeedsConfirm},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := testDaemon(t, tt.rules, demoCap{id: "a.b"})
			fe := &fakeEvents{}
			d.events = fe
			d.handle(tt.req)
			if len(fe.events) != 1 || fe.events[0].Kind != tt.want {
				t.Fatalf("audited %+v, want one %q event", fe.events, tt.want)
			}
			if fe.events[0].Source != "daemon" || fe.events[0].Cap != "a.b" {
				t.Fatalf("event = %+v", fe.events[0])
			}
		})
	}
}

func TestHandleRunPersistsHistory(t *testing.T) {
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	d := testDaemon(t, allow, demoCap{id: "a.b"})
	d.historyPath = filepath.Join(t.TempDir(), "actions.json")

	if resp := d.handle(protocol.Request{Op: protocol.OpRun, Cap: "a.b"}); resp.Status != protocol.StatusRan {
		t.Fatalf("run = %+v", resp)
	}
	// The one-shot CLI must see the daemon's action: it is persisted, not
	// memory-only — the two planes share one recent-action world.
	saved, err := policyfile.LoadActionLog(d.historyPath)
	if err != nil {
		t.Fatalf("loading persisted history: %v", err)
	}
	if len(saved) != 1 || saved[0].Signature != core.ActionSignature("a.b", nil) {
		t.Fatalf("persisted history = %+v", saved)
	}
}

func TestHandleRunMarksValidationErrorsRetryable(t *testing.T) {
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}

	t.Run("validation rejection is retryable", func(t *testing.T) {
		d := testDaemon(t, allow, demoCap{id: "a.b", runErr: core.Validationf("arg %q must be >= 1", "workspace")})
		resp := d.handle(protocol.Request{Op: protocol.OpRun, Cap: "a.b"})
		if resp.Status != protocol.StatusError || !resp.Retryable {
			t.Fatalf("validation failure = %+v, want error with Retryable", resp)
		}
	})

	t.Run("runtime failure is not retryable", func(t *testing.T) {
		d := testDaemon(t, allow, demoCap{id: "a.b", runErr: errors.New("hyprctl: connection refused")})
		resp := d.handle(protocol.Request{Op: protocol.OpRun, Cap: "a.b"})
		if resp.Status != protocol.StatusError || resp.Retryable {
			t.Fatalf("runtime failure = %+v, want error without Retryable (re-asking the model cannot fix the world)", resp)
		}
	})
}

func TestHandleEvaluateDoesNotRun(t *testing.T) {
	ran := false
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	d := testDaemon(t, allow, demoCap{id: "a.b", ran: &ran})
	resp := d.handle(protocol.Request{Op: protocol.OpEvaluate, Cap: "a.b"})
	if resp.Status != protocol.StatusDecision || resp.Text != "allow" {
		t.Fatalf("evaluate = %+v", resp)
	}
	if ran {
		t.Fatal("evaluate is a dry run — it must not execute the capability")
	}
}
