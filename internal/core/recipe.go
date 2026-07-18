package core

import (
	"fmt"
	"sort"
	"strings"
)

// Step is one action in a recipe: a capability to invoke with fixed arguments.
type Step struct {
	Capability string // capability ID, e.g. "workspace.switch"
	Args       Args   // arguments passed to that capability
}

// Recipe is a named, deterministic sequence of capability calls — a macro for
// the things you do daily ("set up my work environment"). A recipe contains no
// shell and no free logic: every step is a typed capability from the allowlist,
// so a recipe can do nothing a capability cannot already do on its own.
//
// Recipes do NOT bypass the permission policy: when a recipe runs, each step is
// still evaluated by the same gate as a hand-typed call. A recipe is a
// convenience, never an escalation.
type Recipe struct {
	Name        string
	Description string
	Steps       []Step
}

// Validate checks a recipe is safe and runnable against a registry: it has a
// name, at least one step, every step names a registered capability, and it
// passes the lifecycle guard. It does not validate step arguments — each
// capability does that on its own Run, returning a corrective error rather than
// executing on bad input.
func (r Recipe) Validate(reg *Registry) error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("recipe has no name")
	}
	if len(r.Steps) == 0 {
		return fmt.Errorf("recipe %q has no steps", r.Name)
	}
	for i, s := range r.Steps {
		if _, ok := reg.Get(s.Capability); !ok {
			return fmt.Errorf("recipe %q step %d: unknown capability %q", r.Name, i+1, s.Capability)
		}
	}
	return r.guardLifecycle()
}

// dangerousVerbs are command names that, appearing as a whole word in a step's
// arguments, can restart or kill hyprvalet's host — the agent process or the
// Hyprland session around it — and risk wedging the machine in a restart loop.
var dangerousVerbs = map[string]bool{
	"pkill":     true,
	"killall":   true,
	"kill":      true,
	"reboot":    true,
	"poweroff":  true,
	"shutdown":  true,
	"halt":      true,
	"loginctl":  true,
	"systemctl": true,
}

// guardLifecycle refuses a recipe whose steps would restart or kill the host.
func (r Recipe) guardLifecycle() error {
	return checkLifecycle(r.Steps, fmt.Sprintf("recipe %q", r.Name))
}

// checkLifecycle refuses a sequence of steps that would restart or kill the
// host — the agent process or the Hyprland session around it. It is shared by
// recipes (installer-authored) and plans (model-generated): both must be unable
// to wedge the machine in a restart loop. Conservative defense-in-depth, not the
// permission boundary (that is the policy gate). Lesson from hermes-agent. The
// kind argument ("recipe %q" / "plan") labels the error's source.
func checkLifecycle(steps []Step, kind string) error {
	for i, s := range steps {
		for _, v := range s.Args {
			low := strings.ToLower(v)
			for _, f := range strings.Fields(low) {
				if dangerousVerbs[f] {
					return lifecycleErr(kind, i, s, v, f)
				}
			}
			// Referencing the agent binary itself, or telling the compositor to
			// exit, are self-destruct paths a step must never contain.
			if strings.Contains(low, "hyprvalet") {
				return lifecycleErr(kind, i, s, v, "hyprvalet")
			}
			if strings.Contains(low, "hyprctl") && strings.Contains(low, "exit") {
				return lifecycleErr(kind, i, s, v, "hyprctl exit")
			}
		}
	}
	return nil
}

func lifecycleErr(kind string, idx int, s Step, val, matched string) error {
	return fmt.Errorf(
		"%s step %d (%s): refused by lifecycle guard — argument %q may restart or kill hyprvalet's host (matched %q)",
		kind, idx+1, s.Capability, val, matched)
}

// RecipeBook is the set of loaded recipes, keyed by name. Like the capability
// Registry it rejects duplicate names, so two recipe files can never silently
// shadow each other.
type RecipeBook struct {
	recipes map[string]Recipe
}

// NewRecipeBook returns an empty book.
func NewRecipeBook() *RecipeBook {
	return &RecipeBook{recipes: make(map[string]Recipe)}
}

// Add validates a recipe against the registry and files it. It errors on an
// invalid recipe or a duplicate name.
func (b *RecipeBook) Add(r Recipe, reg *Registry) error {
	if err := r.Validate(reg); err != nil {
		return err
	}
	if _, exists := b.recipes[r.Name]; exists {
		return fmt.Errorf("recipe %q already defined", r.Name)
	}
	b.recipes[r.Name] = r
	return nil
}

// Get returns a recipe by name.
func (b *RecipeBook) Get(name string) (Recipe, bool) {
	r, ok := b.recipes[name]
	return r, ok
}

// List returns all recipes, sorted by name for stable output.
func (b *RecipeBook) List() []Recipe {
	out := make([]Recipe, 0, len(b.recipes))
	for _, r := range b.recipes {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
