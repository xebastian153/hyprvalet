// Package protocol is the typed contract between the hyprvalet daemon and its
// clients — one shared package so both speak exactly the same shapes, never a
// hand-matched pair of encoders. Messages are JSON, one Request then one
// Response per exchange, streamed over a Unix socket.
package protocol

import "github.com/xebastian153/hyprvalet/internal/core"

// Op is the kind of a request.
type Op string

const (
	OpPing Op = "ping" // liveness + a capability count
	OpList Op = "list" // enumerate capabilities
	OpRun  Op = "run"  // run one capability by id with args
)

// Request is a typed command from a client to the daemon.
type Request struct {
	Op   Op                `json:"op"`
	Cap  string            `json:"cap,omitempty"`
	Args map[string]string `json:"args,omitempty"`
}

// Status is the outcome class of a response, so a client can branch without
// parsing prose.
type Status string

const (
	StatusPong         Status = "pong"
	StatusCaps         Status = "caps"
	StatusRan          Status = "ran"
	StatusDenied       Status = "denied"        // policy denied the action
	StatusNeedsConfirm Status = "needs_confirm" // would run only with a human's approval
	StatusError        Status = "error"         // malformed request, unknown capability, run failure
)

// Response is the daemon's reply to one Request.
type Response struct {
	Status Status    `json:"status"`
	Text   string    `json:"text,omitempty"`  // human-readable result or reason
	Error  string    `json:"error,omitempty"` // set when Status is error
	Caps   []CapInfo `json:"caps,omitempty"`  // set when Status is caps
	Count  int       `json:"count,omitempty"` // capability count, for pong
}

// CapInfo is the wire view of a capability — the core.Capability interface
// flattened to serializable fields.
type CapInfo struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	Access      string   `json:"access"`
	Risk        string   `json:"risk"`
	Params      []string `json:"params"`
}

// CapInfoOf flattens a core.Capability for the wire.
func CapInfoOf(c core.Capability) CapInfo {
	return CapInfo{
		ID:          c.ID(),
		Description: c.Description(),
		Access:      string(c.Access()),
		Risk:        c.Risk().String(),
		Params:      c.Params(),
	}
}
