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
	arm     core.ArmState
	session core.SessionAllow
	history []core.ActionRecord
	mailbox chan command
	log     *log.Logger
}

// New builds a daemon, seeding its in-memory state from the persisted files so
// it inherits the current arming and session grants.
func New(reg *core.Registry, rules core.PolicyRules, logger *log.Logger) *Daemon {
	now := time.Now()
	arm, _ := policyfile.LoadArmState(policyfile.ArmStatePath(), now)
	session, _ := policyfile.LoadSessionAllow(policyfile.SessionAllowPath())
	history, _ := policyfile.LoadActionLog(policyfile.ActionLogPath())
	return &Daemon{
		reg:     reg,
		rules:   rules,
		arm:     arm,
		session: session,
		history: core.PruneActions(history, now, core.DoomLoopWindow),
		mailbox: make(chan command),
		log:     logger,
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

// serveConn reads requests from one connection and writes a reply for each,
// forwarding every request to the actor via the mailbox. It never touches daemon
// state directly, so it needs no synchronization.
func (d *Daemon) serveConn(conn net.Conn) {
	defer conn.Close()
	dec := json.NewDecoder(conn)
	enc := json.NewEncoder(conn)
	for {
		var req protocol.Request
		if err := dec.Decode(&req); err != nil {
			return // EOF or a broken connection
		}
		reply := make(chan protocol.Response, 1)
		d.mailbox <- command{req: req, reply: reply}
		if err := enc.Encode(<-reply); err != nil {
			return
		}
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
	default:
		return protocol.Response{Status: protocol.StatusError, Error: fmt.Sprintf("unknown op %q", req.Op)}
	}
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
		return protocol.Response{Status: protocol.StatusDenied, Text: fmt.Sprintf("policy denies %q", cap.ID())}
	case core.DecisionAsk:
		if !req.Approved {
			return protocol.Response{Status: protocol.StatusNeedsConfirm, Text: fmt.Sprintf("%q needs confirmation", cap.ID())}
		}
	}

	sig := core.ActionSignature(cap.ID(), args)
	if !req.Approved && core.IsDoomLoop(d.history, sig, now, core.DoomLoopWindow, core.DoomLoopThreshold) {
		return protocol.Response{Status: protocol.StatusNeedsConfirm, Text: fmt.Sprintf("%q is repeating; needs confirmation", cap.ID())}
	}

	out, err := cap.Run(context.Background(), args)
	if err != nil {
		return protocol.Response{Status: protocol.StatusError, Error: err.Error()}
	}
	d.history = append(d.history, core.ActionRecord{Signature: sig, At: now})
	if out == "" {
		out = cap.ID() + " ok"
	}
	return protocol.Response{Status: protocol.StatusRan, Text: out}
}
