// Package daemon is the long-lived hyprvalet control plane. It is a driving
// adapter: it receives typed requests over a Unix socket and drives the core,
// exactly as the one-shot CLI does, but stays resident and holds state in
// memory. A single goroutine owns all mutable state and every request is
// funneled to it through a mailbox channel — the goroutines+channels actor model
// the project's non-negotiables require, so there are no locks.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xebastian153/hyprvalet/internal/adapters/policyfile"
	"github.com/xebastian153/hyprvalet/internal/core"
	"github.com/xebastian153/hyprvalet/internal/protocol"
)

// SocketPath returns the daemon's Unix socket path: under $XDG_RUNTIME_DIR
// (per-user, wiped on logout), falling back to a per-user temp directory.
func SocketPath() string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("hyprvalet-%d", os.Getuid()))
	}
	return filepath.Join(dir, "hyprvalet", "daemon.sock")
}

// Listen creates the Unix socket for the daemon, making its directory 0700 and
// removing any stale socket left by a previous run.
func Listen(socketPath string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return net.Listen("unix", socketPath)
}

// command is one request routed to the actor goroutine, with a channel to carry
// the reply back to the connection that sent it.
type command struct {
	req   protocol.Request
	reply chan protocol.Response
}

// Daemon holds the registry, policy, and the mutable state (arming, session
// grants, action history) that the one-shot CLI keeps in files. Only the actor
// goroutine (Run) touches the mutable fields.
type Daemon struct {
	reg     *core.Registry
	rules   core.PolicyRules
	planner core.PlannerPort // multi-step reasoning (ask/plan); read-only after New
	llm     core.LLMPort     // single-intent reasoning (ask); read-only after New
	// llmStrong is the escalation tier: the larger model used when a client
	// reports the default one failed to correct its own mistake. May be nil
	// (no escalation available). Read-only after New.
	llmStrong core.LLMPort
	events    core.EventStore // audit log; may be nil (no auditing)
	arm       core.ArmState
	session   core.SessionAllow
	history   []core.ActionRecord
	// historyPath is where executed actions are persisted so the one-shot CLI
	// shares the daemon's recent-action world; empty means in-memory only.
	historyPath string
	mailbox     chan command
	log         *log.Logger
}

// New builds a daemon, seeding its in-memory state from the persisted files so
// it inherits the current arming and session grants. The reasoning ports (LLM
// and planner) and the audit store are injected so the core stays behind its
// interfaces; production passes one Ollama client for both reasoning ports.
func New(reg *core.Registry, rules core.PolicyRules, planner core.PlannerPort, llm, llmStrong core.LLMPort, events core.EventStore, logger *log.Logger) *Daemon {
	now := time.Now()
	arm, _ := policyfile.LoadArmState(policyfile.ArmStatePath(), now)
	session, _ := policyfile.LoadSessionAllow(policyfile.SessionAllowPath())
	historyPath := policyfile.ActionLogPath()
	history, _ := policyfile.LoadActionLog(historyPath)
	return &Daemon{
		reg:         reg,
		rules:       rules,
		planner:     planner,
		llm:         llm,
		llmStrong:   llmStrong,
		events:      events,
		arm:         arm,
		session:     session,
		history:     core.PruneActions(history, now, core.DoomLoopWindow),
		historyPath: historyPath,
		mailbox:     make(chan command),
		log:         logger,
	}
}

// recent returns the agent's episodic memory for the reasoning ports: the
// latest audit events inside the memory window. Memory is a nice-to-have, never
// a gate — any failure reads as an empty past. Safe off the actor goroutine:
// the store is read-only state and reads its file independently.
func (d *Daemon) recent() []core.Event {
	if d.events == nil {
		return nil
	}
	list, err := d.events.Tail(core.MemoryEvents)
	if err != nil {
		d.log.Printf("warning: could not read recent events: %v", err)
		return nil
	}
	return core.RecentEvents(list, time.Now(), core.MemoryWindow)
}

// emit appends one event to the audit log. Auditing is an observer, never a
// gate: a failed append is logged and the action's outcome stands.
func (d *Daemon) emit(kind core.EventKind, cap string, args core.Args, detail string) {
	if d.events == nil {
		return
	}
	e := core.Event{At: time.Now(), Source: "daemon", Kind: kind, Cap: cap, Args: args, Detail: detail}
	if err := d.events.Append(e); err != nil {
		d.log.Printf("warning: could not append audit event: %v", err)
	}
}

// Run is the two-layer loop's outer layer: a single actor goroutine selecting
// over the mailbox (client commands), a timer (expiring arming grants and
// pruning history), and cancellation. It owns the state, so handle needs no
// locks. Connections are accepted in a separate goroutine that forwards their
// requests here. Run returns when ctx is cancelled.
func (d *Daemon) Run(ctx context.Context, ln net.Listener) error {
	go d.acceptLoop(ln)

	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()

	d.log.Printf("listening on %s (%d capabilities)", ln.Addr(), len(d.reg.List()))
	for {
		select {
		case cmd := <-d.mailbox:
			cmd.reply <- d.handle(cmd.req)
		case <-tick.C:
			now := time.Now()
			d.arm.Prune(now)
			d.history = core.PruneActions(d.history, now, core.DoomLoopWindow)
		case <-ctx.Done():
			d.log.Printf("shutting down")
			return nil
		}
	}
}

func (d *Daemon) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return // listener closed on shutdown
		}
		go d.serveConn(conn)
	}
}

// serveConn reads requests from one connection and writes a reply for each. It
// never touches mutable daemon state directly, so it needs no synchronization.
func (d *Daemon) serveConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req protocol.Request
		if err := dec.Decode(&req); err != nil {
			return // EOF or a broken connection
		}
		if err := enc.Encode(d.dispatch(req)); err != nil {
			return
		}
	}
}

// dispatch routes one request. Ask/Plan reason with the LLM — slow I/O that
// touches no mutable state — so they run HERE, on the connection goroutine, and
// only dip into the actor (through the mailbox) for the fast policy evaluations
// they need. Keeping a multi-second model call off the actor is what lets a
// concurrent ping still answer instantly. Every state-owning op is funneled to
// the actor, which processes them one at a time with no locks.
func (d *Daemon) dispatch(req protocol.Request) protocol.Response {
	switch req.Op {
	case protocol.OpAsk:
		return d.reasonAsk(req)
	case protocol.OpPlan:
		return d.reasonPlan(req)
	default:
		reply := make(chan protocol.Response, 1)
		d.mailbox <- command{req: req, reply: reply}
		return <-reply
	}
}

// handle processes one request. It runs ON the actor goroutine, so it is the
// only place daemon state is read or written — no synchronization needed.
func (d *Daemon) handle(req protocol.Request) protocol.Response {
	switch req.Op {
	case protocol.OpPing:
		return protocol.Response{Status: protocol.StatusPong, Count: len(d.reg.List())}
	case protocol.OpList:
		list := d.reg.List()
		caps := make([]protocol.CapInfo, 0, len(list))
		for _, c := range list {
			caps = append(caps, protocol.CapInfoOf(c))
		}
		return protocol.Response{Status: protocol.StatusCaps, Caps: caps}
	case protocol.OpRun:
		return d.handleRun(req)
	case protocol.OpEvaluate:
		return d.handleEvaluate(req)
	default:
		return protocol.Response{Status: protocol.StatusError, Error: fmt.Sprintf("unknown op %q", req.Op)}
	}
}

// handleEvaluate returns the current policy decision for a capability without
// running it. It runs ON the actor, so it reads the live arming and session
// state — the reasoning goroutine calls it (via the mailbox) to bind a plan
// against a world only the actor owns.
func (d *Daemon) handleEvaluate(req protocol.Request) protocol.Response {
	cap, ok := d.reg.Get(req.Cap)
	if !ok {
		return protocol.Response{Status: protocol.StatusError, Error: fmt.Sprintf("unknown capability %q", req.Cap)}
	}
	dec := core.Decide(d.rules, d.arm, d.session, cap, time.Now())
	return protocol.Response{Status: protocol.StatusDecision, Text: dec.String()}
}

// handleRun runs one capability. A resident daemon must never auto-run something
// that needs a human, so an unapproved Ask or doom-loop is refused with a
// needs_confirm the client can act on. The client obtains approval and re-sends
// with Approved set; the daemon re-evaluates (so a state change since the prompt
// still blocks) and runs. Approval lets an Ask or doom-loop through but never
// overrides a policy Deny.
func (d *Daemon) handleRun(req protocol.Request) protocol.Response {
	cap, ok := d.reg.Get(req.Cap)
	if !ok {
		return protocol.Response{Status: protocol.StatusError, Error: fmt.Sprintf("unknown capability %q", req.Cap)}
	}
	now := time.Now()
	args := core.Args(req.Args)

	switch core.Decide(d.rules, d.arm, d.session, cap, now) {
	case core.DecisionDeny:
		// Absolute: approval cannot widen what the policy forbids.
		d.emit(core.EventDenied, cap.ID(), args, "policy denies it")
		return protocol.Response{Status: protocol.StatusDenied, Text: fmt.Sprintf("policy denies %q", cap.ID())}
	case core.DecisionAsk:
		if !req.Approved {
			d.emit(core.EventNeedsConfirm, cap.ID(), args, "awaiting approval")
			return protocol.Response{Status: protocol.StatusNeedsConfirm, Text: fmt.Sprintf("%q needs confirmation", cap.ID())}
		}
	}

	sig := core.ActionSignature(cap.ID(), args)
	if !req.Approved && core.IsDoomLoop(d.history, sig, now, core.DoomLoopWindow, core.DoomLoopThreshold) {
		d.emit(core.EventNeedsConfirm, cap.ID(), args, "repeating action; awaiting approval")
		return protocol.Response{Status: protocol.StatusNeedsConfirm, Text: fmt.Sprintf("%q is repeating; needs confirmation", cap.ID())}
	}

	out, err := cap.Run(context.Background(), args)
	if err != nil {
		d.emit(core.EventFailed, cap.ID(), args, err.Error())
		return protocol.Response{Status: protocol.StatusError, Error: err.Error(), Retryable: core.IsValidation(err)}
	}
	d.history = append(d.history, core.ActionRecord{Signature: sig, At: now})
	// Persist the history so the one-shot CLI's doom-loop check sees actions the
	// daemon ran — the two planes share one recent-action world.
	if d.historyPath != "" {
		if err := policyfile.SaveActionLog(d.historyPath, d.history); err != nil {
			d.log.Printf("warning: could not persist action history: %v", err)
		}
	}
	if out == "" {
		out = cap.ID() + " ok"
	}
	d.emit(core.EventRan, cap.ID(), args, out)
	return protocol.Response{Status: protocol.StatusRan, Text: out}
}

// reasonAsk maps a natural-language request to a single typed intent and returns
// it as a one-step, policy-bound plan — never running it. It runs on the
// connection goroutine (the model call is slow and stateless); only the final
// policy binding touches the actor. A hallucinated capability is caught by
// ResolveIntent against the allowlist before it can reach a preview.
func (d *Daemon) reasonAsk(req protocol.Request) protocol.Response {
	request := strings.TrimSpace(req.Text)
	if request == "" {
		return protocol.Response{Status: protocol.StatusError, Error: "empty request"}
	}
	llm := d.llm
	if req.Escalate && d.llmStrong != nil {
		llm = d.llmStrong
		d.log.Printf("escalating %q to the stronger model", request)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	intent, err := llm.Interpret(ctx, request, d.reg.List(), d.recent())
	cancel()
	if err != nil {
		return protocol.Response{Status: protocol.StatusError, Error: fmt.Sprintf("reasoning failed: %v", err)}
	}
	cap, err := core.ResolveIntent(d.reg, intent)
	if err != nil {
		// No action is a valid outcome, not a wire error. A conversational
		// Reply rides along — words only, nothing executes — and the exchange
		// is recorded so later turns can refer back to it.
		if intent.Reply != "" {
			d.emit(core.EventReplied, "", nil, fmt.Sprintf("user: %s / assistant: %s", request, intent.Reply))
		}
		return protocol.Response{Status: protocol.StatusPlanned, Reasoning: intent.Reasoning, Reply: intent.Reply}
	}
	return protocol.Response{
		Status:    protocol.StatusPlanned,
		Reasoning: intent.Reasoning,
		Plan: []protocol.PlanStep{{
			Cap:      cap.ID(),
			Args:     map[string]string(intent.Args),
			Decision: d.evaluate(cap.ID()),
		}},
	}
}

// reasonPlan maps a natural-language request to an ordered, validated,
// policy-bound plan and returns it without running anything. Like reasonAsk it
// reasons off the actor and binds each step through it. The plan is validated
// against the allowlist and the lifecycle guard, so no previewed step can name
// an unregistered capability or try to restart the host.
func (d *Daemon) reasonPlan(req protocol.Request) protocol.Response {
	request := strings.TrimSpace(req.Text)
	if request == "" {
		return protocol.Response{Status: protocol.StatusError, Error: "empty request"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	plan, err := d.planner.Plan(ctx, request, d.reg.List(), d.recent())
	cancel()
	if err != nil {
		return protocol.Response{Status: protocol.StatusError, Error: fmt.Sprintf("planning failed: %v", err)}
	}
	if len(plan.Steps) == 0 {
		// The planner plans; it does not converse. An empty plan falls back to
		// the intent layer, whose structural contract reliably separates talk
		// from action — it may answer conversationally, or even rescue a
		// single action the planner missed. Both outcomes stay inside the
		// same allowlist and gate.
		return d.fallbackToIntent(request, plan.Summary)
	}
	if err := plan.Validate(d.reg); err != nil {
		return protocol.Response{Status: protocol.StatusError, Error: fmt.Sprintf("invalid plan: %v", err)}
	}
	steps := make([]protocol.PlanStep, len(plan.Steps))
	for i, s := range plan.Steps {
		steps[i] = protocol.PlanStep{
			Cap:      s.Capability,
			Args:     map[string]string(s.Args),
			Decision: d.evaluate(s.Capability),
		}
	}
	return protocol.Response{Status: protocol.StatusPlanned, Summary: plan.Summary, Plan: steps}
}

// fallbackToIntent handles a request the planner produced no steps for: the
// intent layer decides whether it is conversation (Reply — words only, nothing
// executes) or a single action the planner missed (returned as a one-step,
// policy-bound plan). With neither, it is an honest no-match.
func (d *Daemon) fallbackToIntent(request, summary string) protocol.Response {
	if d.llm == nil {
		return protocol.Response{Status: protocol.StatusPlanned, Summary: summary}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	intent, err := d.llm.Interpret(ctx, request, d.reg.List(), d.recent())
	cancel()
	if err != nil {
		// The plan already answered "nothing to do"; a broken fallback must
		// not turn that into a failure.
		return protocol.Response{Status: protocol.StatusPlanned, Summary: summary}
	}
	if cap, err := core.ResolveIntent(d.reg, intent); err == nil {
		return protocol.Response{
			Status: protocol.StatusPlanned,
			Plan: []protocol.PlanStep{{
				Cap:      cap.ID(),
				Args:     map[string]string(intent.Args),
				Decision: d.evaluate(cap.ID()),
			}},
		}
	}
	if intent.Reply != "" {
		d.emit(core.EventReplied, "", nil, fmt.Sprintf("user: %s / assistant: %s", request, intent.Reply))
	}
	return protocol.Response{Status: protocol.StatusPlanned, Summary: summary, Reply: intent.Reply}
}

// evaluate asks the actor goroutine for the current policy decision on a
// capability without running it. It is how the reasoning goroutine binds a plan
// against the live arming and session state the actor alone owns.
func (d *Daemon) evaluate(capID string) string {
	reply := make(chan protocol.Response, 1)
	d.mailbox <- command{req: protocol.Request{Op: protocol.OpEvaluate, Cap: capID}, reply: reply}
	return (<-reply).Text
}
