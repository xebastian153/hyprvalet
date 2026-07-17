package core

import (
	"context"
	"fmt"
)

// Intent is a structured interpretation of a natural-language request: the one
// capability the reasoning layer chose and the arguments it filled. It is the
// ONLY thing that layer may emit — never a shell string. An empty Capability
// means the model found nothing in the allowlist matching the request.
type Intent struct {
	Capability string // chosen capability ID; "" if nothing matched
	Args       Args   // arguments the model filled for that capability
	Reasoning  string // optional one-line rationale, for transparency and logs
}

// LLMPort is the reasoning boundary of the hexagon. An adapter (e.g. a local
// Ollama model) implements it; the core depends only on this interface, never on
// a concrete model, prompt format, or HTTP client. Interpret maps a
// natural-language request to a single typed Intent, choosing only from the
// capabilities it is given — so the caller controls what the model may pick.
//
// A returned error means the reasoning itself failed (model unreachable,
// timeout, unparseable output). A successful call that simply found no match
// returns an Intent with an empty Capability, not an error.
type LLMPort interface {
	Interpret(ctx context.Context, request string, caps []Capability) (Intent, error)
}

// ResolveIntent validates a model-produced Intent against the registry. This is
// the allowlist check that makes a hallucination harmless: a capability the
// model invented is rejected here, before anything runs. It does not validate
// arguments — the capability does that on its own Run, and the corrective error
// from there can be fed back to the model to retry.
func ResolveIntent(reg *Registry, intent Intent) (Capability, error) {
	if intent.Capability == "" {
		return nil, fmt.Errorf("no capability matched the request")
	}
	c, ok := reg.Get(intent.Capability)
	if !ok {
		return nil, fmt.Errorf("model chose %q, which is not a registered capability", intent.Capability)
	}
	return c, nil
}
