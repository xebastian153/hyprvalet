// Command hyprvalet is a typed, permission-gated agent for controlling an
// Omarchy/Hyprland desktop.
//
// M0 (this milestone) has NO LLM. The CLI invokes capabilities directly to
// prove the core loop end to end: a typed capability registry, real adapters
// over hyprctl and the omarchy CLI, argument validation, and a permission gate
// driven by an installer-owned policy (allow / ask / deny) with temporal arming
// of dangerous capabilities. Everything the LLM layer will later drive already
// works here by hand.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/xebastian153/hyprvalet/internal/adapters/hypr"
	"github.com/xebastian153/hyprvalet/internal/adapters/ollama"
	"github.com/xebastian153/hyprvalet/internal/adapters/omarchy"
	"github.com/xebastian153/hyprvalet/internal/adapters/policyfile"
	"github.com/xebastian153/hyprvalet/internal/adapters/recipefile"
	"github.com/xebastian153/hyprvalet/internal/core"
)

func buildRegistry() *core.Registry {
	reg := core.NewRegistry()
	all := append(hypr.Capabilities(), omarchy.Capabilities()...)
	for _, c := range all {
		if err := reg.Register(c); err != nil {
			// A collision in the allowlist is a build-time mistake, not a
			// warning to scroll past: fail loudly rather than run with a
			// capability silently missing.
			fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
			os.Exit(1)
		}
	}
	return reg
}

func main() {
	reg := buildRegistry()

	rules, err := policyfile.Load(policyfile.ConfigPath())
	if err != nil {
		// Fail closed: a broken policy file must never run permissively.
		fmt.Fprintf(os.Stderr, "policy error: %v\n", err)
		os.Exit(1)
	}

	args := os.Args[1:]
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage()
		return
	}

	switch args[0] {
	case "list":
		listCaps(reg, rules)
	case "armed":
		listArmed()
	case "arm":
		requireArg(args, "arm", "<capability>")
		armCap(reg, rules, args[1])
	case "disarm":
		requireArg(args, "disarm", "<capability>")
		disarmCap(reg, args[1])
	case "recipe":
		recipeCmd(reg, rules, args[1:])
	case "ask":
		askCmd(reg, rules, args[1:])
	case "run":
		requireArg(args, "run", "<capability> [key=value ...]")
		runCap(reg, rules, args[1], args[2:])
	default:
		// Shortcut form: `hyprvalet <capability> [key=value ...]`.
		runCap(reg, rules, args[0], args[1:])
	}
}

func requireArg(args []string, verb, shape string) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: hyprvalet %s %s\n", verb, shape)
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("hyprvalet — typed, permission-gated control for Omarchy/Hyprland")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  hyprvalet list                       show capabilities and their policy")
	fmt.Println("  hyprvalet <capability> [key=value]   run a capability (shortcut for run)")
	fmt.Println("  hyprvalet run <capability> [k=v ...] run a capability")
	fmt.Println("  hyprvalet arm <capability>           grant a bounded window to a gated capability")
	fmt.Println("  hyprvalet disarm <capability>        revoke a grant immediately")
	fmt.Println("  hyprvalet armed                      list currently-armed capabilities")
	fmt.Println("  hyprvalet recipe list                list recipes")
	fmt.Println("  hyprvalet recipe show <name>         preview a recipe's steps")
	fmt.Println("  hyprvalet recipe run <name>          run a recipe (each step is still gated)")
	fmt.Println("  hyprvalet ask \"<request>\"            map natural language to one capability (local LLM)")
	fmt.Println()
	fmt.Println("Policy file (installer-owned):")
	fmt.Printf("  %s\n", policyfile.ConfigPath())
	fmt.Println("Recipes directory:")
	fmt.Printf("  %s\n", recipefile.RecipesDir())
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  hyprvalet workspace.switch workspace=3")
	fmt.Println("  hyprvalet arm app.open && hyprvalet app.open cmd=firefox")
	fmt.Println("  hyprvalet omarchy.run args=\"restart waybar\"")
}

func listCaps(reg *core.Registry, rules core.PolicyRules) {
	for _, c := range reg.List() {
		r := rules.Resolve(c)
		marker := ""
		if r.RequiresArming {
			marker = " arming"
		}
		fmt.Printf("%-28s [%-9s risk=%-9s policy=%-5s%s] %s\n",
			c.ID(), c.Access(), c.Risk(), r.Decision, marker, c.Description())
		if len(c.Params()) > 0 {
			fmt.Printf("%-28s   params: %s\n", "", strings.Join(c.Params(), ", "))
		}
	}
}

func runCap(reg *core.Registry, rules core.PolicyRules, id string, rest []string) {
	cap, ok := reg.Get(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown capability %q — run 'hyprvalet list'\n", id)
		os.Exit(2)
	}

	args, err := parseArgs(rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}

	now := time.Now()
	armState, err := policyfile.LoadArmState(policyfile.ArmStatePath(), now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading arm state: %v\n", err)
		os.Exit(1)
	}

	switch gate(cap, args, rules, armState, now) {
	case gateDenied:
		os.Exit(1)
	case gateDeclined:
		fmt.Println("aborted")
		return
	}

	out, err := cap.Run(context.Background(), args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if out != "" {
		fmt.Println(out)
	}
}

// gateResult is the outcome of evaluating the permission policy for one call.
type gateResult int

const (
	gateProceed  gateResult = iota // policy allows, or the human confirmed
	gateDenied                     // policy denies; a reason was printed to stderr
	gateDeclined                   // policy asked and the human said no
)

// gate evaluates the policy for one capability call and performs any
// confirmation prompt. It prints the denial reason itself; the caller decides
// what a denied or declined outcome means for its flow (a single run aborts;
// a recipe stops). It is shared so recipes and hand-typed calls gate identically
// — a recipe is never a permission bypass.
func gate(cap core.Capability, args core.Args, rules core.PolicyRules, armState core.ArmState, now time.Time) gateResult {
	switch core.Evaluate(rules, armState, cap, now) {
	case core.DecisionDeny:
		r := rules.Resolve(cap)
		if r.RequiresArming && !armState.IsArmed(cap.ID(), now) {
			fmt.Fprintf(os.Stderr,
				"denied: %q requires arming — run 'hyprvalet arm %s' to grant %s\n",
				cap.ID(), cap.ID(), rules.ArmFor(cap))
		} else {
			fmt.Fprintf(os.Stderr, "denied by policy: %q\n", cap.ID())
		}
		return gateDenied
	case core.DecisionAsk:
		if confirm(cap, args) {
			return gateProceed
		}
		return gateDeclined
	default:
		return gateProceed
	}
}

func armCap(reg *core.Registry, rules core.PolicyRules, id string) {
	cap, ok := reg.Get(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown capability %q — run 'hyprvalet list'\n", id)
		os.Exit(2)
	}
	if !rules.Resolve(cap).RequiresArming {
		fmt.Fprintf(os.Stderr,
			"note: %q is not arming-gated by the current policy; arming it has no effect\n", id)
	}

	now := time.Now()
	path := policyfile.ArmStatePath()
	state, err := policyfile.LoadArmState(path, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading arm state: %v\n", err)
		os.Exit(1)
	}
	dur := rules.ArmFor(cap)
	state.Arm(id, now, dur)
	if err := policyfile.SaveArmState(path, state, now); err != nil {
		fmt.Fprintf(os.Stderr, "error saving arm state: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("armed %s for %s (until %s)\n", id, dur, now.Add(dur).Format("15:04:05"))
}

func disarmCap(reg *core.Registry, id string) {
	if _, ok := reg.Get(id); !ok {
		fmt.Fprintf(os.Stderr, "unknown capability %q — run 'hyprvalet list'\n", id)
		os.Exit(2)
	}
	now := time.Now()
	path := policyfile.ArmStatePath()
	state, err := policyfile.LoadArmState(path, now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading arm state: %v\n", err)
		os.Exit(1)
	}
	state.Disarm(id)
	if err := policyfile.SaveArmState(path, state, now); err != nil {
		fmt.Fprintf(os.Stderr, "error saving arm state: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("disarmed %s\n", id)
}

func listArmed() {
	now := time.Now()
	state, err := policyfile.LoadArmState(policyfile.ArmStatePath(), now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading arm state: %v\n", err)
		os.Exit(1)
	}
	if len(state) == 0 {
		fmt.Println("no capabilities armed")
		return
	}
	ids := make([]string, 0, len(state))
	for id := range state {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		until := state[id]
		// Grants are stored in UTC; show the wall clock in local time so this
		// matches what `arm` printed when the grant was created.
		fmt.Printf("%-28s %s remaining (until %s)\n",
			id, until.Sub(now).Round(time.Second), until.Local().Format("15:04:05"))
	}
}

func recipeCmd(reg *core.Registry, rules core.PolicyRules, args []string) {
	// Recipes load lazily, and fail closed: a broken recipe blocks recipe
	// commands, not the rest of the CLI.
	book, err := recipefile.Load(recipefile.RecipesDir(), reg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "recipe error: %v\n", err)
		os.Exit(1)
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hyprvalet recipe <list|show|run> [name]")
		os.Exit(2)
	}
	switch args[0] {
	case "list":
		listRecipes(book)
	case "show":
		requireArg(args, "recipe show", "<name>")
		printPlan(getRecipe(book, args[1]))
	case "run":
		requireArg(args, "recipe run", "<name>")
		runRecipe(reg, rules, getRecipe(book, args[1]))
	default:
		fmt.Fprintf(os.Stderr, "unknown recipe subcommand %q (want list|show|run)\n", args[0])
		os.Exit(2)
	}
}

func getRecipe(book *core.RecipeBook, name string) core.Recipe {
	r, ok := book.Get(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown recipe %q — run 'hyprvalet recipe list'\n", name)
		os.Exit(2)
	}
	return r
}

func listRecipes(book *core.RecipeBook) {
	recipes := book.List()
	if len(recipes) == 0 {
		fmt.Printf("no recipes — add *.toml files under %s\n", recipefile.RecipesDir())
		return
	}
	for _, r := range recipes {
		fmt.Printf("%-20s %s (%d steps)\n", r.Name, r.Description, len(r.Steps))
	}
}

func printPlan(r core.Recipe) {
	fmt.Printf("%s — %s\n", r.Name, r.Description)
	for i, s := range r.Steps {
		fmt.Printf("  %d. %s %s\n", i+1, s.Capability, formatArgs(s.Args))
	}
}

func formatArgs(a core.Args) string {
	parts := make([]string, 0, len(a))
	for k, v := range a {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func runRecipe(reg *core.Registry, rules core.PolicyRules, r core.Recipe) {
	now := time.Now()
	armState, err := policyfile.LoadArmState(policyfile.ArmStatePath(), now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading arm state: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("running recipe %q:\n", r.Name)
	printPlan(r)

	n := len(r.Steps)
	for i, s := range r.Steps {
		// Validated at load, so Get always succeeds; the check is defensive.
		cap, ok := reg.Get(s.Capability)
		if !ok {
			fmt.Fprintf(os.Stderr, "recipe %q step %d/%d: unknown capability %q\n", r.Name, i+1, n, s.Capability)
			os.Exit(1)
		}
		switch gate(cap, s.Args, rules, armState, now) {
		case gateDenied, gateDeclined:
			fmt.Fprintf(os.Stderr, "recipe %q aborted at step %d/%d (%s)\n", r.Name, i+1, n, s.Capability)
			os.Exit(1)
		}
		out, err := cap.Run(context.Background(), s.Args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "recipe %q failed at step %d/%d (%s): %v\n", r.Name, i+1, n, s.Capability, err)
			os.Exit(1)
		}
		if out == "" {
			out = s.Capability + " ok"
		}
		fmt.Printf("  [%d/%d] %s\n", i+1, n, out)
	}
	fmt.Printf("recipe %q done\n", r.Name)
}

func askCmd(reg *core.Registry, rules core.PolicyRules, rest []string) {
	request := strings.TrimSpace(strings.Join(rest, " "))
	if request == "" {
		fmt.Fprintln(os.Stderr, "usage: hyprvalet ask \"<what you want>\"")
		os.Exit(2)
	}

	// The model may choose from every capability; the allowlist and the gate,
	// not the prompt, are what keep a wrong choice safe.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	intent, err := ollama.Default().Interpret(ctx, request, reg.List())
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "reasoning failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "(is ollama running? try: systemctl status ollama)")
		os.Exit(1)
	}

	cap, err := core.ResolveIntent(reg, intent)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		if intent.Reasoning != "" {
			fmt.Fprintf(os.Stderr, "(model said: %s)\n", intent.Reasoning)
		}
		os.Exit(1)
	}

	// Transparency: show what the model decided before anything runs.
	fmt.Printf("understood: %s %s\n", cap.ID(), formatArgs(intent.Args))
	if intent.Reasoning != "" {
		fmt.Printf("  reasoning: %s\n", intent.Reasoning)
	}

	now := time.Now()
	armState, err := policyfile.LoadArmState(policyfile.ArmStatePath(), now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading arm state: %v\n", err)
		os.Exit(1)
	}
	switch gate(cap, intent.Args, rules, armState, now) {
	case gateDenied:
		os.Exit(1)
	case gateDeclined:
		fmt.Println("aborted")
		return
	}

	out, err := cap.Run(context.Background(), intent.Args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if out != "" {
		fmt.Println(out)
	}
}

func parseArgs(rest []string) (core.Args, error) {
	args := core.Args{}
	for _, kv := range rest {
		k, v, ok := strings.Cut(kv, "=")
		if !ok {
			return nil, fmt.Errorf("bad argument %q — expected key=value", kv)
		}
		args[strings.TrimSpace(k)] = v
	}
	return args, nil
}

func confirm(cap core.Capability, args core.Args) bool {
	fmt.Printf("About to run %s (%s, risk=%s) with %v\n",
		cap.ID(), cap.Access(), cap.Risk(), map[string]string(args))
	fmt.Print("Proceed? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
