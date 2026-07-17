// Command hyprvalet is a typed, permission-gated agent for controlling an
// Omarchy/Hyprland desktop.
//
// M0 (this milestone) has NO LLM. The CLI invokes capabilities directly to
// prove the core loop end to end: a typed capability registry, real adapters
// over hyprctl and the omarchy CLI, argument validation, and a permission gate
// that asks before running Confirm-tier actions. Everything the LLM layer will
// later drive already works here by hand.
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/SebasDevMag/hyprvalet/internal/adapters/hypr"
	"github.com/SebasDevMag/hyprvalet/internal/adapters/omarchy"
	"github.com/SebasDevMag/hyprvalet/internal/core"
)

func buildRegistry() *core.Registry {
	reg := core.NewRegistry()
	all := append(hypr.Capabilities(), omarchy.Capabilities()...)
	for _, c := range all {
		if err := reg.Register(c); err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
		}
	}
	return reg
}

func main() {
	reg := buildRegistry()
	args := os.Args[1:]

	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage()
		return
	}

	switch args[0] {
	case "list":
		listCaps(reg)
	case "run":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: hyprvalet run <capability> [key=value ...]")
			os.Exit(2)
		}
		runCap(reg, args[1], args[2:])
	default:
		// Shortcut form: `hyprvalet <capability> [key=value ...]`.
		runCap(reg, args[0], args[1:])
	}
}

func usage() {
	fmt.Println("hyprvalet — typed, permission-gated control for Omarchy/Hyprland")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  hyprvalet list")
	fmt.Println("  hyprvalet run <capability> [key=value ...]")
	fmt.Println("  hyprvalet <capability> [key=value ...]   (shortcut)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  hyprvalet workspace.switch workspace=3")
	fmt.Println("  hyprvalet window.move_to_workspace workspace=2")
	fmt.Println("  hyprvalet app.open cmd=firefox")
	fmt.Println("  hyprvalet omarchy.run args=\"restart waybar\"")
}

func listCaps(reg *core.Registry) {
	for _, c := range reg.List() {
		fmt.Printf("%-28s [%-9s risk=%-8s] %s\n", c.ID(), c.Access(), c.Risk(), c.Description())
		if len(c.Params()) > 0 {
			fmt.Printf("%-28s   params: %s\n", "", strings.Join(c.Params(), ", "))
		}
	}
}

func runCap(reg *core.Registry, id string, rest []string) {
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

	switch cap.Risk() {
	case core.RiskForbidden:
		fmt.Fprintf(os.Stderr, "capability %q is forbidden\n", id)
		os.Exit(1)
	case core.RiskConfirm:
		// Permission gate: Confirm-tier actions need explicit human approval.
		if !confirm(cap, args) {
			fmt.Println("aborted")
			return
		}
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
