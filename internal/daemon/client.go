package daemon

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/xebastian153/hyprvalet/internal/protocol"
)

// RunViaDaemon runs a capability through the daemon with the two-phase
// confirmation flow: it asks once, and if the daemon replies needs_confirm it
// calls confirm; on approval it re-sends the same request with Approved set. The
// daemon re-evaluates the approved call, so nothing is trusted blindly. Each
// phase is its own request/response, so the daemon keeps no per-connection state.
func RunViaDaemon(socketPath, cap string, args map[string]string, confirm func(reason string) bool) (protocol.Response, error) {
	resp, err := Send(socketPath, protocol.Request{Op: protocol.OpRun, Cap: cap, Args: args})
	if err != nil {
		return protocol.Response{}, err
	}
	if resp.Status != protocol.StatusNeedsConfirm {
		return resp, nil
	}
	if !confirm(resp.Text) {
		return protocol.Response{Status: protocol.StatusDenied, Text: "declined"}, nil
	}
	return Send(socketPath, protocol.Request{Op: protocol.OpRun, Cap: cap, Args: args, Approved: true})
}

// AskViaDaemon has the daemon reason a single intent from a natural-language
// request and return it unexecuted (a one-step, policy-bound plan). Execution is
// the caller's job — a separate OpRun through RunViaDaemon's confirm flow.
// escalate asks for the daemon's stronger model — set on the final corrective
// attempt, after the default model failed to fix its own mistake.
func AskViaDaemon(socketPath, request string, escalate bool) (protocol.Response, error) {
	return Send(socketPath, protocol.Request{Op: protocol.OpAsk, Text: request, Escalate: escalate})
}

// PlanViaDaemon has the daemon reason an ordered, validated, policy-bound plan
// from a natural-language request and return it unexecuted. The caller previews
// it, and runs each step with RunStep after one confirmation.
func PlanViaDaemon(socketPath, request string) (protocol.Response, error) {
	return Send(socketPath, protocol.Request{Op: protocol.OpPlan, Text: request})
}

// RunStep runs one plan step through the daemon. approved carries whether a
// human confirmed the plan — a plan of purely policy-allowed steps runs
// unapproved, so a doom-loop can still interpose. The daemon re-evaluates
// every step against live state (TOCTOU) either way; approval never outlives
// the world it was given in.
func RunStep(socketPath, cap string, args map[string]string, approved bool) (protocol.Response, error) {
	return Send(socketPath, protocol.Request{Op: protocol.OpRun, Cap: cap, Args: args, Approved: approved})
}

// Send is the thin client: it opens a connection to the daemon at socketPath,
// sends one request, and returns the one response. A refused connection is
// reported with a hint that the daemon may not be running.
func Send(socketPath string, req protocol.Request) (protocol.Response, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return protocol.Response{}, fmt.Errorf("connecting to daemon at %s: %w (is it running? start it with 'hyprvalet daemon')", socketPath, err)
	}
	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return protocol.Response{}, fmt.Errorf("sending request: %w", err)
	}
	var resp protocol.Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return protocol.Response{}, fmt.Errorf("reading response: %w", err)
	}
	return resp, nil
}
