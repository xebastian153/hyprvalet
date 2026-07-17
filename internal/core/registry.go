package core

import (
	"fmt"
	"sort"
)

// Registry is the allowlist of capabilities. Anything not registered is
// impossible to invoke — not "hopefully blocked by a filter". That allowlist
// property is the core safety guarantee: the set of things the agent can ever
// do is exactly the set registered here.
type Registry struct {
	caps map[string]Capability
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{caps: make(map[string]Capability)}
}

// Register adds a capability. It errors on a duplicate ID so two adapters can
// never silently shadow each other.
func (r *Registry) Register(c Capability) error {
	if _, exists := r.caps[c.ID()]; exists {
		return fmt.Errorf("capability %q already registered", c.ID())
	}
	r.caps[c.ID()] = c
	return nil
}

// Get returns a capability by ID.
func (r *Registry) Get(id string) (Capability, bool) {
	c, ok := r.caps[id]
	return c, ok
}

// List returns all capabilities, sorted by ID for stable output.
func (r *Registry) List() []Capability {
	out := make([]Capability, 0, len(r.caps))
	for _, c := range r.caps {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}
