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
	"github.com/xebastian153/hyprvalet/internal/adapters/omarchy"
	"github.com/xebastian153/hyprvalet/internal/adapters/policyfile"
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
	fmt.Println()
	fmt.Println("Policy file (installer-owned):")
	fmt.Printf("  %s\n", policyfile.ConfigPath())
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

	switch core.Evaluate(rules, armState, cap, now) {
	case core.DecisionDeny:
		r := rules.Resolve(cap)
		if r.RequiresArming && !armState.IsArmed(id, now) {
			fmt.Fprintf(os.Stderr,
				"denied: %q requires arming — run 'hyprvalet arm %s' to grant %s\n",
				id, id, rules.ArmFor(cap))
		} else {
			fmt.Fprintf(os.Stderr, "denied by policy: %q\n", id)
		}
		os.Exit(1)
	case core.DecisionAsk:
		if !confirm(cap, args) {
			fmt.Println("aborted")
			return
		}
	case core.DecisionAllow:
		// proceed
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
