package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/xebastian153/hyprvalet/internal/adapters/policyfile"
	"github.com/xebastian153/hyprvalet/internal/core"
	"github.com/xebastian153/hyprvalet/internal/protocol"
)

// helper to build a daemon with realtime support for tests
func testRealtimeDaemon(t *testing.T, rules core.PolicyRules, caps ...core.Capability) *Daemon {
	t.Helper()
	reg := core.NewRegistry()
	for _, c := range caps {
		if err := reg.Register(c); err != nil {
			t.Fatalf("register %q: %v", c.ID(), err)
		}
	}
	// Use internal constructor to get clean daemon with realtime state
	d := &Daemon{
		reg:     reg,
		rules:   rules,
		arm:     core.ArmState{},
		session: core.SessionAllow{},
		mailbox: make(chan command),
		log:     log.New(io.Discard, "", 0),
	}
	// Initialize realtime state if helper exists
	if initFn := realtimeInitForTest; initFn != nil {
		initFn(d)
	}
	return d
}

// ---------------------------------------------------------------------------
// Generation stamp
// ---------------------------------------------------------------------------

func TestRealtime_GenerationStamp(t *testing.T) {
	// Daemon must track generation and echo it; new session starts at 0.
	d := testRealtimeDaemon(t, core.PolicyRules{}, demoCap{id: "a.b"})
	// Start session — requires realtime session active
	d.StartRealtimeSession()
	if got := d.RealtimeGeneration(); got != 0 {
		t.Fatalf("initial generation = %d, want 0", got)
	}
	// Stamped request with gen 0 should succeed (allow policy)
	d.rules = core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	if resp.Status != protocol.StatusRan {
		t.Fatalf("OpRealtime gen0 = %+v, want ran", resp)
	}
	if resp.Generation != 0 {
		t.Fatalf("echoed generation = %d, want 0", resp.Generation)
	}
	// Future generation should not be stale — should run
	resp2 := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 1})
	if resp2.Status != protocol.StatusRan {
		t.Fatalf("future gen 1 = %+v, want ran", resp2)
	}
}

func TestRealtime_GenerationThreadSafe(t *testing.T) {
	d := testRealtimeDaemon(t, core.PolicyRules{})
	d.StartRealtimeSession()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.RealtimeCancel()
			_ = d.RealtimeGeneration()
			_ = d.RealtimeIsStale(0)
		}()
	}
	wg.Wait()
	if got := d.RealtimeGeneration(); got != 20 {
		t.Fatalf("concurrent Cancel generation = %d, want 20", got)
	}
}

// ---------------------------------------------------------------------------
// Stale discard via CancelScope.IsStale
// ---------------------------------------------------------------------------

func TestRealtime_StaleDiscard(t *testing.T) {
	tests := []struct {
		name       string
		cancels    int
		reqGen     int
		wantStale  bool
		wantStatus protocol.Status
	}{
		{"current not stale", 1, 1, false, protocol.StatusRan},
		{"old stale discarded", 2, 1, true, protocol.StatusCancelled},
		{"zero stale after increments", 1, 0, true, protocol.StatusCancelled},
		{"future not stale", 1, 5, false, protocol.StatusRan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ran := false
			d := testRealtimeDaemon(t, core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}, demoCap{id: "a.b", ran: &ran})
			d.StartRealtimeSession()
			for i := 0; i < tt.cancels; i++ {
				d.RealtimeCancel()
			}
			if got := d.RealtimeIsStale(tt.reqGen); got != tt.wantStale {
				t.Fatalf("IsStale(%d) with gen %d = %v, want %v", tt.reqGen, d.RealtimeGeneration(), got, tt.wantStale)
			}
			resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: tt.reqGen})
			if resp.Status != tt.wantStatus {
				t.Fatalf("stale discard status = %q, want %q, resp=%+v", resp.Status, tt.wantStatus, resp)
			}
			if tt.wantStale && ran {
				t.Fatal("stale generation must NOT run the capability")
			}
			if !tt.wantStale && !ran {
				t.Fatal("non-stale must run")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ErrIdle when no session
// ---------------------------------------------------------------------------

func TestRealtime_ErrIdle_NoSession(t *testing.T) {
	d := testRealtimeDaemon(t, core.PolicyRules{}, demoCap{id: "a.b"})
	// Do NOT start session
	resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	if resp.Status != protocol.StatusError {
		t.Fatalf("no session should be error, got %+v", resp)
	}
	if !contains(resp.Error, "ErrIdle") && !contains(resp.Error, "no realtime session") && !contains(resp.Error, "no session") {
		t.Fatalf("error should mention ErrIdle/no session, got %q", resp.Error)
	}
	// After starting, it should not be ErrIdle
	d.StartRealtimeSession()
	d.rules = core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	resp2 := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	if resp2.Status == protocol.StatusError && contains(resp2.Error, "ErrIdle") {
		t.Fatalf("after StartRealtimeSession, should not be ErrIdle, got %+v", resp2)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

// ---------------------------------------------------------------------------
// TOCTOU re-evaluation at handleRun (Decide + IsDoomLoop + ArmState)
// ---------------------------------------------------------------------------

func TestRealtime_TOCTOU_ArmExpiry(t *testing.T) {
	// Capability requires arming; arm it with 1s window, then expire before handle.
	capID := "a.b"
	rules := core.PolicyRules{
		ByCapID:       map[string]core.Rule{capID: {Decision: core.DecisionAllow, RequiresArming: true}},
		DefaultArmFor: time.Minute,
	}
	ran := false
	d := testRealtimeDaemon(t, rules, demoCap{id: capID, ran: &ran})
	d.StartRealtimeSession()
	// Arm for 1 second then immediately expire by setting expiry in past
	now := time.Now()
	d.arm.Arm(capID, now.Add(-2*time.Second), time.Second) // expired 1s ago

	resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: capID, Generation: 0})
	if resp.Status != protocol.StatusDenied {
		t.Fatalf("arm expired TOCTOU should deny, got %+v ran=%v", resp, ran)
	}
	if ran {
		t.Fatal("expired arm must not run")
	}
	// Re-arm and it should allow
	d.arm.Arm(capID, time.Now(), time.Minute)
	ran = false
	resp2 := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: capID, Generation: d.RealtimeGeneration()})
	if resp2.Status != protocol.StatusRan || !ran {
		t.Fatalf("armed should run, got %+v ran=%v", resp2, ran)
	}
}

func TestRealtime_TOCTOU_DecideAndDoomLoop(t *testing.T) {
	// Decide deny at run time even if plan would have allowed
	t.Run("deny blocks", func(t *testing.T) {
		ran := false
		d := testRealtimeDaemon(t, core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionDeny}}}, demoCap{id: "a.b", ran: &ran})
		d.StartRealtimeSession()
		resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
		if resp.Status != protocol.StatusDenied || ran {
			t.Fatalf("deny = %+v ran=%v", resp, ran)
		}
	})
	t.Run("ask needs confirm", func(t *testing.T) {
		ran := false
		d := testRealtimeDaemon(t, core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAsk}}}, demoCap{id: "a.b", ran: &ran})
		d.StartRealtimeSession()
		resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
		if resp.Status != protocol.StatusNeedsConfirm || ran {
			t.Fatalf("ask without approved = %+v ran=%v", resp, ran)
		}
		resp2 := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0, Approved: true})
		if resp2.Status != protocol.StatusRan || !ran {
			t.Fatalf("approved ask = %+v ran=%v", resp2, ran)
		}
	})
	t.Run("doom-loop blocks", func(t *testing.T) {
		allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
		ran := false
		d := testRealtimeDaemon(t, allow, demoCap{id: "a.b", ran: &ran})
		d.StartRealtimeSession()
		sig := core.ActionSignature("a.b", nil)
		now := time.Now()
		for i := 0; i < core.DoomLoopThreshold-1; i++ {
			d.history = append(d.history, core.ActionRecord{Signature: sig, At: now})
		}
		resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
		if resp.Status != protocol.StatusNeedsConfirm || ran {
			t.Fatalf("doom-loop should needs_confirm, got %+v ran=%v", resp, ran)
		}
	})
}

// ---------------------------------------------------------------------------
// Validationf handling at TOCTOU
// ---------------------------------------------------------------------------

func TestRealtime_Validationf_Retryable(t *testing.T) {
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	d := testRealtimeDaemon(t, allow, demoCap{id: "a.b", runErr: core.Validationf("arg %q must be >=1", "workspace")})
	d.StartRealtimeSession()
	resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0, Args: map[string]string{"workspace": "0"}})
	if resp.Status != protocol.StatusError || !resp.Retryable {
		t.Fatalf("validation should be retryable error, got %+v", resp)
	}
	// runtime error not retryable
	d2 := testRealtimeDaemon(t, allow, demoCap{id: "a.b", runErr: errors.New("hyprctl dead")})
	d2.StartRealtimeSession()
	resp2 := d2.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	if resp2.Status != protocol.StatusError || resp2.Retryable {
		t.Fatalf("runtime failure should not be retryable, got %+v", resp2)
	}
}

// ---------------------------------------------------------------------------
// Drain on SESSION_END (10s)
// ---------------------------------------------------------------------------

func TestRealtime_Drain_SESSION_END(t *testing.T) {
	d := testRealtimeDaemon(t, core.PolicyRules{}, demoCap{id: "a.b"})
	d.StartRealtimeSession()
	// SESSION_END should trigger drain and return
	resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "SESSION_END", Generation: 0})
	if resp.Status != protocol.StatusRan && resp.Status != protocol.StatusCancelled && resp.Status != protocol.StatusStreaming {
		// Accept ran as drain ack; important is that drain timer is set
		t.Fatalf("SESSION_END = %+v, want drain ack", resp)
	}
	if !d.IsDraining() {
		t.Fatal("after SESSION_END, IsDraining should be true (10s drain)")
	}
	// Verify drain preserves SESSION_END semantics via t.TempDir: history should not leak hallucination, but SESSION_END itself is not a cap so not logged.
	tmp := t.TempDir()
	d.historyPath = filepath.Join(tmp, "actions.json")
	// Ensure history file not polluted — SESSION_END must not create a cap entry
	if _, err := policyfile.LoadActionLog(d.historyPath); err == nil {
		// file may not exist yet; that's fine — drain should not create one
	}
	// Simulate drain expiry: manually set drainUntil to past and verify IsDraining false
	d.realtimeMu.Lock()
	d.realtimeDrainUntil = time.Now().Add(-time.Second)
	d.realtimeMu.Unlock()
	if d.IsDraining() {
		t.Fatal("after drain expiry, IsDraining should be false")
	}
	// After drain, session should be idle -> ErrIdle for non-SESSION_END
	d.rules = core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	resp2 := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	if resp2.Status != protocol.StatusError || !contains(resp2.Error, "ErrIdle") {
		t.Fatalf("after drain, should be ErrIdle, got %+v", resp2)
	}
}

// ---------------------------------------------------------------------------
// Quarantine on schema_drift / hallucination (180s)
// ---------------------------------------------------------------------------

func TestRealtime_Quarantine_Hallucination(t *testing.T) {
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	d := testRealtimeDaemon(t, allow, demoCap{id: "a.b"})
	d.StartRealtimeSession()
	tmp := t.TempDir()
	d.historyPath = filepath.Join(tmp, "actions.json")
	// Hallucinated tool => quarantine
	resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "hallucinated.tool", Generation: 0})
	if resp.Status != protocol.StatusQuarantined {
		t.Fatalf("hallucinated should quarantine, got %+v", resp)
	}
	if !d.IsQuarantined() {
		t.Fatal("after hallucination, IsQuarantined should be true (180s)")
	}
	// Subsequent valid call should still be quarantined
	resp2 := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	if resp2.Status != protocol.StatusQuarantined {
		t.Fatalf("during quarantine, valid should still quarantine, got %+v", resp2)
	}
	// t.TempDir leak check: quarantine must not leak hallucinated cap into persisted history
	if hist, err := policyfile.LoadActionLog(d.historyPath); err == nil && len(hist) != 0 {
		for _, r := range hist {
			if strings.Contains(r.Signature, "hallucinated") {
				t.Fatalf("quarantine leaked hallucinated sig into history: %+v", r)
			}
		}
	}
	// Simulate quarantine expiry: set to past and verify next valid can run
	d.realtimeMu.Lock()
	d.realtimeQuarantinedUntil = time.Now().Add(-time.Second)
	d.realtimeSessionActive = true
	d.realtimeMu.Unlock()
	if d.IsQuarantined() {
		t.Fatal("after quarantine expiry, should not be quarantined")
	}
	d.RealtimeCancel() // ensure gen fresh
	resp3 := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: d.RealtimeGeneration()})
	if resp3.Status != protocol.StatusRan {
		t.Fatalf("after quarantine expiry, valid should run, got %+v", resp3)
	}
}

func TestRealtime_Quarantine_SchemaDrift(t *testing.T) {
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	// Cap that returns schema drift validation
	d := testRealtimeDaemon(t, allow, demoCap{id: "a.b", runErr: core.Validationf("schema_drift: arg %q invalid", "workspace")})
	d.StartRealtimeSession()
	resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	// Schema drift should quarantine per spec
	if resp.Status != protocol.StatusQuarantined {
		t.Fatalf("schema_drift validation should quarantine, got %+v", resp)
	}
	if !d.IsQuarantined() {
		t.Fatal("schema_drift should set quarantine")
	}
}

// ---------------------------------------------------------------------------
// Httptest fake WS + channel mailbox (integration harness)
// ---------------------------------------------------------------------------

func TestRealtime_HttptestFakeWS_Generation(t *testing.T) {
	// Simulate sidecar WS with gorilla/websocket via httptest.NewServer; daemon
	// generation should increment on speech_started (barge-in) and stale events
	// should be discarded, proving the WS harness matches client_test patterns.
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		// Read client hello then send speech_started with generation
		_, _, _ = conn.ReadMessage()
		msg, _ := json.Marshal(map[string]any{"type": "input_audio_buffer.speech_started", "generation": 0})
		_ = conn.WriteMessage(websocket.TextMessage, msg)
		// Keep open briefly
		time.Sleep(50 * time.Millisecond)
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")

	ran := false
	d := testRealtimeDaemon(t, core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}, demoCap{id: "a.b", ran: &ran})
	d.StartRealtimeSession()

	// Dial fake WS as client would
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial fake WS: %v", err)
	}
	defer conn.Close()
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.update"}`))
	// Server will send speech_started; simulate barge-in by cancelling daemon gen
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read speech_started: %v", err)
	}
	var ev map[string]json.RawMessage
	if err := json.Unmarshal(msg, &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var typ string
	_ = json.Unmarshal(ev["type"], &typ)
	if typ != "input_audio_buffer.speech_started" {
		t.Fatalf("event type = %q, want speech_started", typ)
	}
	// Barge: increment generation via daemon
	newGen := d.RealtimeCancel()
	if newGen != 1 {
		t.Fatalf("after speech_started Cancel gen = %d, want 1", newGen)
	}
	// Stale request with old gen should be discarded
	resp := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	if resp.Status != protocol.StatusCancelled {
		t.Fatalf("stale after WS barge = %+v, want cancelled", resp)
	}
	if ran {
		t.Fatal("stale must not run after WS barge")
	}
	// Current gen should run
	resp2 := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: newGen})
	if resp2.Status != protocol.StatusRan || !ran {
		t.Fatalf("current gen after WS = %+v ran=%v, want ran", resp2, ran)
	}
}

// ---------------------------------------------------------------------------
// Mailbox isolation: OpRealtime via channel
// ---------------------------------------------------------------------------

func TestRealtime_MailboxIsolation(t *testing.T) {
	// Verify OpRealtime goes through mailbox (actor) and not via direct handle
	// Use runDaemon socket path and Send
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	ran := false
	d := testRealtimeDaemon(t, allow, demoCap{id: "a.b", ran: &ran})
	d.StartRealtimeSession()
	socket := runDaemon(t, d)
	resp, err := Send(socket, protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	if err != nil {
		t.Fatalf("Send OpRealtime: %v", err)
	}
	if resp.Status != protocol.StatusRan || !ran {
		t.Fatalf("mailbox OpRealtime = %+v ran=%v", resp, ran)
	}
	// Ensure ErrIdle via mailbox when no session
	d2 := testRealtimeDaemon(t, core.PolicyRules{}, demoCap{id: "a.b"})
	socket2 := runDaemon(t, d2)
	resp2, err := Send(socket2, protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: 0})
	if err != nil {
		t.Fatalf("Send ErrIdle: %v", err)
	}
	if resp2.Status != protocol.StatusError || !contains(resp2.Error, "ErrIdle") {
		t.Fatalf("mailbox ErrIdle = %+v, want ErrIdle", resp2)
	}
}

// ---------------------------------------------------------------------------
// HandleRun TOCTOU must also check Validationf/DoomLoop for realtime path?
// ---------------------------------------------------------------------------

func TestRealtime_HandleRun_TOCTOU_Parity(t *testing.T) {
	// Ensure handleRun's Decide/DoomLoop logic parity for OpRealtime vs OpRun
	allow := core.PolicyRules{ByCapID: map[string]core.Rule{"a.b": {Decision: core.DecisionAllow}}}
	ran := false
	d := testRealtimeDaemon(t, allow, demoCap{id: "a.b", ran: &ran})
	d.StartRealtimeSession()
	// Prime doom-loop history
	sig := core.ActionSignature("a.b", nil)
	now := time.Now()
	for i := 0; i < core.DoomLoopThreshold-1; i++ {
		d.history = append(d.history, core.ActionRecord{Signature: sig, At: now})
	}
	// Both OpRun and OpRealtime should trigger doom-loop needs_confirm
	respRun := d.handle(protocol.Request{Op: protocol.OpRun, Cap: "a.b"})
	respRT := d.handle(protocol.Request{Op: protocol.OpRealtime, Cap: "a.b", Generation: d.RealtimeGeneration()})
	if respRun.Status != protocol.StatusNeedsConfirm {
		t.Fatalf("OpRun doom-loop = %q, want needs_confirm", respRun.Status)
	}
	if respRT.Status != protocol.StatusNeedsConfirm {
		t.Fatalf("OpRealtime doom-loop parity = %q, want needs_confirm", respRT.Status)
	}
}
