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
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/xebastian153/hyprvalet/internal/adapters/audio"
	"github.com/xebastian153/hyprvalet/internal/adapters/eventlog"
	"github.com/xebastian153/hyprvalet/internal/adapters/fallback"
	"github.com/xebastian153/hyprvalet/internal/adapters/groq"
	"github.com/xebastian153/hyprvalet/internal/adapters/hypr"
	"github.com/xebastian153/hyprvalet/internal/adapters/media"
	"github.com/xebastian153/hyprvalet/internal/adapters/mic"
	"github.com/xebastian153/hyprvalet/internal/adapters/ollama"
	"github.com/xebastian153/hyprvalet/internal/adapters/omarchy"
	"github.com/xebastian153/hyprvalet/internal/adapters/policyfile"
	"github.com/xebastian153/hyprvalet/internal/adapters/recipefile"
	"github.com/xebastian153/hyprvalet/internal/adapters/tts"
	"github.com/xebastian153/hyprvalet/internal/adapters/whisper"
	"github.com/xebastian153/hyprvalet/internal/core"
	"github.com/xebastian153/hyprvalet/internal/daemon"
	"github.com/xebastian153/hyprvalet/internal/protocol"
)

func buildRegistry() *core.Registry {
	reg := core.NewRegistry()
	all := append(hypr.Capabilities(), omarchy.Capabilities()...)
	all = append(all, media.Capabilities()...)
	all = append(all, audio.Capabilities()...)
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

// events is the process-wide audit log. Auditing is an observer, never a gate:
// a failed append warns and the action's outcome stands.
var events = eventlog.New(eventlog.Path())

// defaultReasoner picks the reasoning provider: Groq (cloud — larger models at
// interactive speed) when GROQ_API_KEY is set, composed with local Ollama as an
// automatic fallback so losing the network never silences the agent; local
// Ollama alone otherwise. The privacy trade is explicit: with Groq, requests
// and their episodic-memory context leave the machine.
func defaultReasoner() fallback.Reasoner {
	local := ollama.Default()
	if groq.Available() {
		return fallback.New(groq.Default(), local)
	}
	return local
}

// strongReasoner picks the escalation tier for the corrective loop, with the
// same cloud-with-local-fallback composition.
func strongReasoner() core.LLMPort {
	if groq.Available() {
		return fallback.New(groq.Strong(), ollama.Strong())
	}
	return ollama.Strong()
}

// emitEvent records what became of one attempted capability call.
func emitEvent(kind core.EventKind, cap string, args core.Args, detail string) {
	e := core.Event{At: time.Now(), Source: "cli", Kind: kind, Cap: cap, Args: args, Detail: detail}
	if err := events.Append(e); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not append audit event: %v\n", err)
	}
}

// recentEvents returns the agent's episodic memory for the reasoning layer.
// Memory is a nice-to-have, never a gate: any failure reads as an empty past.
func recentEvents() []core.Event {
	list, err := events.Tail(core.MemoryEvents)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not read recent events: %v\n", err)
		return nil
	}
	return core.RecentEvents(list, time.Now(), core.MemoryWindow)
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
	case "log":
		logCmd(args[1:])
	case "ask":
		askCmd(reg, rules, args[1:])
	case "plan":
		planCmd(reg, rules, args[1:], false)
	case "do":
		planCmd(reg, rules, args[1:], true)
	case "voice":
		voiceCmd()
	case "daemon":
		daemonCmd(reg, rules)
	case "ping":
		pingCmd()
	case "ctl":
		ctlCmd(args[1:])
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
	fmt.Println("  hyprvalet log [count]                show the audit trail (attempted actions + outcomes)")
	fmt.Println("  hyprvalet ask \"<request>\"            map natural language to one capability (local LLM)")
	fmt.Println("  hyprvalet plan \"<request>\"           preview a multi-step plan without running it")
	fmt.Println("  hyprvalet do \"<request>\"             plan, confirm once, then execute step by step")
	fmt.Println("  hyprvalet daemon                     run the long-lived daemon (Unix socket)")
	fmt.Println("  hyprvalet ping                       check the daemon is alive")
	fmt.Println("  hyprvalet ctl run <cap> [k=v ...]    run a capability via the daemon (confirms if needed)")
	fmt.Println("  hyprvalet ctl ask \"<request>\"        reason one capability in the daemon, then run it")
	fmt.Println("  hyprvalet ctl plan \"<request>\"       preview a daemon-reasoned plan (nothing runs)")
	fmt.Println("  hyprvalet ctl do \"<request>\"         reason, confirm once, then run the plan via the daemon")
	fmt.Println("  hyprvalet voice                      speak a request (records until Enter, runs via daemon)")
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

	dc := loadDecisionCtx(rules)
	switch gate(cap, args, &dc) {
	case gateDenied:
		os.Exit(1)
	case gateDeclined:
		fmt.Println("aborted")
		return
	}

	out, err := execCap(cap, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println(out)
}

// stepPause is the breather between consecutive plan/recipe steps. Back-to-back
// desktop mutations (two workspace switches) execute faster than the eye can
// follow; a short pause makes each step visible without feeling sluggish.
const stepPause = 600 * time.Millisecond

// gateResult is the outcome of evaluating the permission policy for one call.
type gateResult int

const (
	gateProceed  gateResult = iota // policy allows, or the human confirmed
	gateDenied                     // policy denies; a reason was printed to stderr
	gateDeclined                   // policy asked and the human said no
)

// decisionCtx bundles everything a permission decision needs at one instant: the
// policy, the current arming and session grants, the recent-action history, and
// where to persist new grants and records. It is built once per command and
// shared by every gated call in it (by pointer), so all of a command's decisions
// see the same world and each recorded action is visible to the next.
type decisionCtx struct {
	rules       core.PolicyRules
	arm         core.ArmState
	session     core.SessionAllow
	sessionPath string
	history     []core.ActionRecord
	historyPath string
	now         time.Time
}

func loadDecisionCtx(rules core.PolicyRules) decisionCtx {
	now := time.Now()
	arm, err := policyfile.LoadArmState(policyfile.ArmStatePath(), now)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading arm state: %v\n", err)
		os.Exit(1)
	}
	sessionPath := policyfile.SessionAllowPath()
	session, err := policyfile.LoadSessionAllow(sessionPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading session grants: %v\n", err)
		os.Exit(1)
	}
	historyPath := policyfile.ActionLogPath()
	history, err := policyfile.LoadActionLog(historyPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading action log: %v\n", err)
		os.Exit(1)
	}
	return decisionCtx{
		rules:       rules,
		arm:         arm,
		session:     session,
		sessionPath: sessionPath,
		history:     core.PruneActions(history, now, core.DoomLoopWindow),
		historyPath: historyPath,
		now:         now,
	}
}

// recordAction appends this execution to the history (in memory and on disk) so
// later gated calls — in this command and later invocations — can see the loop.
func (dc *decisionCtx) recordAction(sig string) {
	dc.history = append(dc.history, core.ActionRecord{Signature: sig, At: dc.now})
	if err := policyfile.SaveActionLog(dc.historyPath, dc.history); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record action: %v\n", err)
	}
}

// gate evaluates the policy (with session grants) for one capability call and
// performs any prompt. It prints the denial reason itself; the caller decides
// what a denied or declined outcome means for its flow. Shared so recipes,
// plans, and hand-typed calls gate identically — never a permission bypass. An
// "always" answer records a session grant so the same action stops prompting.
func gate(cap core.Capability, args core.Args, dc *decisionCtx) gateResult {
	switch core.Decide(dc.rules, dc.arm, dc.session, cap, dc.now) {
	case core.DecisionDeny:
		if r := dc.rules.Resolve(cap); r.RequiresArming && !dc.arm.IsArmed(cap.ID(), dc.now) {
			fmt.Fprintf(os.Stderr,
				"denied: %q requires arming — run 'hyprvalet arm %s' to grant %s\n",
				cap.ID(), cap.ID(), dc.rules.ArmFor(cap))
			emitEvent(core.EventDenied, cap.ID(), args, "requires arming")
		} else {
			fmt.Fprintf(os.Stderr, "denied by policy: %q\n", cap.ID())
			emitEvent(core.EventDenied, cap.ID(), args, "policy denies it")
		}
		return gateDenied
	case core.DecisionAsk:
		switch promptDecision(cap, args) {
		case answerAlways:
			dc.session.Allow(cap.ID())
			if err := policyfile.SaveSessionAllow(dc.sessionPath, dc.session); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not persist session grant: %v\n", err)
			}
		case answerOnce:
			// proceed
		default:
			emitEvent(core.EventDeclined, cap.ID(), args, "human declined")
			return gateDeclined
		}
	}

	// The action is permitted (by policy or approval). Before running it, cut a
	// degenerate loop: the same call firing over and over gets one forced
	// confirmation regardless of policy. Then record it for future checks.
	sig := core.ActionSignature(cap.ID(), args)
	if core.IsDoomLoop(dc.history, sig, dc.now, core.DoomLoopWindow, core.DoomLoopThreshold) {
		fmt.Fprintf(os.Stderr, "warning: %q is repeating — %d+ identical calls (this one included) in the last %s\n",
			cap.ID(), core.DoomLoopThreshold, core.DoomLoopWindow)
		if !promptYes("run it again anyway?") {
			emitEvent(core.EventDeclined, cap.ID(), args, "human declined repeating action")
			return gateDeclined
		}
	}
	dc.recordAction(sig)
	return gateProceed
}

// execCap runs an already-gated capability and audits the outcome. Every CLI
// execution path funnels here so nothing runs without leaving an event behind.
func execCap(cap core.Capability, args core.Args) (string, error) {
	out, err := cap.Run(context.Background(), args)
	if err != nil {
		emitEvent(core.EventFailed, cap.ID(), args, err.Error())
		return "", err
	}
	if out == "" {
		out = cap.ID() + " ok"
	}
	emitEvent(core.EventRan, cap.ID(), args, out)
	return out, nil
}

type decisionAnswer int

const (
	answerNo decisionAnswer = iota
	answerOnce
	answerAlways
)

// promptDecision asks whether to run an Ask-tier action: once, always this
// session, or no (the default). Non-TTY stdin (EOF) reads as no, so the tool
// fails closed when it cannot ask.
func promptDecision(cap core.Capability, args core.Args) decisionAnswer {
	fmt.Printf("About to run %s (%s, risk=%s) with %v\n",
		cap.ID(), cap.Access(), cap.Risk(), map[string]string(args))
	fmt.Print("Allow? [o]nce / [a]lways this session / [N]o ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "o", "once", "y", "yes":
		return answerOnce
	case "a", "always":
		return answerAlways
	default:
		return answerNo
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
	dc := loadDecisionCtx(rules)

	fmt.Printf("running recipe %q:\n", r.Name)
	printPlan(r)

	n := len(r.Steps)
	for i, s := range r.Steps {
		if i > 0 {
			time.Sleep(stepPause)
		}
		// Validated at load, so Get always succeeds; the check is defensive.
		cap, ok := reg.Get(s.Capability)
		if !ok {
			fmt.Fprintf(os.Stderr, "recipe %q step %d/%d: unknown capability %q\n", r.Name, i+1, n, s.Capability)
			os.Exit(1)
		}
		switch gate(cap, s.Args, &dc) {
		case gateDenied, gateDeclined:
			fmt.Fprintf(os.Stderr, "recipe %q aborted at step %d/%d (%s)\n", r.Name, i+1, n, s.Capability)
			os.Exit(1)
		}
		out, err := execCap(cap, s.Args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "recipe %q failed at step %d/%d (%s): %v\n", r.Name, i+1, n, s.Capability, err)
			os.Exit(1)
		}
		fmt.Printf("  [%d/%d] %s\n", i+1, n, out)
	}
	fmt.Printf("recipe %q done\n", r.Name)
}

// maxInterpretAttempts bounds the corrective loop: the first try, one retry fed
// with the validation error, and a final attempt escalated to the stronger
// model (the HYBRID design's second tier). A model that cannot correct itself
// with explicit feedback will not improve by hammering; past the escalation,
// the error goes to the human.
const maxInterpretAttempts = 3

// escalated reports whether attempt i is the escalation tier — the last one.
func escalated(i int) bool { return i == maxInterpretAttempts }

// correctiveRequest re-poses the user's request together with the failure the
// previous attempt earned, so the model corrects a concrete mistake instead of
// guessing blind.
func correctiveRequest(request string, err error) string {
	return fmt.Sprintf("%s\n\n(Your previous attempt was rejected: %v. Choose again from the capability list and fill every argument with a concrete literal value.)",
		request, err)
}

func askCmd(reg *core.Registry, rules core.PolicyRules, rest []string) {
	request := strings.TrimSpace(strings.Join(rest, " "))
	if request == "" {
		fmt.Fprintln(os.Stderr, "usage: hyprvalet ask \"<what you want>\"")
		os.Exit(2)
	}

	// The corrective loop: interpret, gate, run — and when the capability
	// rejects the model's arguments (a ValidationError, the model's own
	// mistake), re-ask once with the error as feedback. Every retry is a brand
	// new intent that walks the full gate again; approval never carries over.
	attempt := request
	for i := 1; ; i++ {
		// The model may choose from every capability; the allowlist and the
		// gate, not the prompt, are what keep a wrong choice safe. The final
		// attempt escalates to the stronger model.
		var llm core.LLMPort = defaultReasoner()
		if escalated(i) {
			fmt.Fprintln(os.Stderr, "escalating to the stronger model")
			llm = strongReasoner()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		intent, err := llm.Interpret(ctx, attempt, reg.List(), recentEvents())
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "reasoning failed: %v\n", err)
			fmt.Fprintln(os.Stderr, "(is ollama running? try: systemctl status ollama)")
			os.Exit(1)
		}

		cap, err := core.ResolveIntent(reg, intent)
		if err != nil {
			// A hallucinated capability is the model's mistake too — feed it
			// back. An empty match ("nothing fits") is an answer, not an error
			// — and a conversational Reply is the answer itself.
			if intent.Capability != "" && i < maxInterpretAttempts {
				fmt.Fprintf(os.Stderr, "model chose badly: %v — asking it to correct\n", err)
				attempt = correctiveRequest(request, err)
				continue
			}
			if intent.Reply != "" {
				fmt.Println(intent.Reply)
				emitEvent(core.EventReplied, "", nil, fmt.Sprintf("user: %s / assistant: %s", request, intent.Reply))
				return
			}
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

		dc := loadDecisionCtx(rules)
		switch gate(cap, intent.Args, &dc) {
		case gateDenied:
			os.Exit(1)
		case gateDeclined:
			fmt.Println("aborted")
			return
		}

		out, err := execCap(cap, intent.Args)
		if err != nil {
			if core.IsValidation(err) && i < maxInterpretAttempts {
				fmt.Fprintf(os.Stderr, "arguments rejected: %v — asking the model to correct\n", err)
				attempt = correctiveRequest(request, err)
				continue
			}
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(out)
		return
	}
}

// planCmd runs the M3 planner. With execute=false ("plan") it previews only;
// with execute=true ("do") it confirms the whole plan once and runs it.
//
// Plan-binding: the plan is evaluated against the policy up front. If any step
// is blocked (denied, or needs arming) the whole plan is refused before anything
// runs — you never execute a plan that cannot complete. One confirmation then
// approves every step you saw in the preview.
func planCmd(reg *core.Registry, rules core.PolicyRules, rest []string, execute bool) {
	request := strings.TrimSpace(strings.Join(rest, " "))
	if request == "" {
		fmt.Fprintln(os.Stderr, "usage: hyprvalet plan|do \"<what you want>\"")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	plan, err := defaultReasoner().Plan(ctx, request, reg.List(), recentEvents())
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "planning failed: %v\n", err)
		fmt.Fprintln(os.Stderr, "(is ollama running? try: systemctl status ollama)")
		os.Exit(1)
	}

	if len(plan.Steps) == 0 {
		// The planner plans; conversation is the intent layer's job. Fall back
		// to it so a chat request still gets an answer.
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		intent, ierr := defaultReasoner().Interpret(ctx, request, reg.List(), recentEvents())
		cancel()
		if ierr == nil && intent.Capability == "" && intent.Reply != "" {
			fmt.Println(intent.Reply)
			emitEvent(core.EventReplied, "", nil, fmt.Sprintf("user: %s / assistant: %s", request, intent.Reply))
			return
		}
		fmt.Println("no plan — the model found nothing it could do for that request")
		return
	}
	if err := plan.Validate(reg); err != nil {
		fmt.Fprintf(os.Stderr, "invalid plan: %v\n", err)
		os.Exit(1)
	}

	// Evaluate every step up front, print the preview, and collect blockers.
	dc := loadDecisionCtx(rules)

	if plan.Summary != "" {
		fmt.Printf("plan: %s\n", plan.Summary)
	}
	var blockers []string
	for i, s := range plan.Steps {
		cap, _ := reg.Get(s.Capability) // safe: Validate confirmed it exists
		d := core.Decide(dc.rules, dc.arm, dc.session, cap, dc.now)
		fmt.Printf("  %d. %s %s  [%s]\n", i+1, s.Capability, formatArgs(s.Args), d)
		if d == core.DecisionDeny {
			if r := dc.rules.Resolve(cap); r.RequiresArming && !dc.arm.IsArmed(cap.ID(), dc.now) {
				blockers = append(blockers, fmt.Sprintf("step %d (%s) needs arming — run 'hyprvalet arm %s'", i+1, cap.ID(), cap.ID()))
			} else {
				blockers = append(blockers, fmt.Sprintf("step %d (%s) is denied by policy", i+1, cap.ID()))
			}
		}
	}

	if len(blockers) > 0 {
		fmt.Fprintln(os.Stderr, "this plan cannot run:")
		for _, b := range blockers {
			fmt.Fprintf(os.Stderr, "  - %s\n", b)
		}
		os.Exit(1)
	}

	if !execute {
		return // `plan` previews only
	}

	if !promptYes(fmt.Sprintf("execute this %d-step plan?", len(plan.Steps))) {
		fmt.Println("aborted")
		return
	}

	n := len(plan.Steps)
	for i, s := range plan.Steps {
		if i > 0 {
			time.Sleep(stepPause)
		}
		cap, _ := reg.Get(s.Capability)
		// Plan-binding re-validation (TOCTOU): the plan was approved as a whole,
		// but re-check each step against the policy the instant before it runs.
		// A grant that expired between approval and now (e.g. an arming window)
		// blocks the step instead of slipping through on the stale approval.
		if core.Decide(dc.rules, dc.arm, dc.session, cap, time.Now()) == core.DecisionDeny {
			emitEvent(core.EventDenied, cap.ID(), s.Args, "state changed since plan approval")
			fmt.Fprintf(os.Stderr, "plan aborted at step %d/%d (%s): no longer permitted (state changed since approval)\n", i+1, n, s.Capability)
			os.Exit(1)
		}
		out, err := execCap(cap, s.Args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "plan failed at step %d/%d (%s): %v\n", i+1, n, s.Capability, err)
			os.Exit(1)
		}
		fmt.Printf("  [%d/%d] %s\n", i+1, n, out)
	}
	fmt.Println("plan done")
}

func daemonCmd(reg *core.Registry, rules core.PolicyRules) {
	socket := daemon.SocketPath()
	ln, err := daemon.Listen(socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot start daemon: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove(socket)

	// Ctrl-C / SIGTERM cancels the context; closing the listener unblocks the
	// accept loop so Run returns cleanly and the socket is removed.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	// One reasoner serves both ports (LLMPort + PlannerPort); a second, stronger
	// one is the escalation tier. Provider selection (Groq with local fallback,
	// or local alone) happens once, here at the edge.
	r := defaultReasoner()
	d := daemon.New(reg, rules, r, r, strongReasoner(), events, log.New(os.Stderr, "hyprvalet-daemon ", log.LstdFlags))
	if err := d.Run(ctx, ln); err != nil {
		fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
		os.Exit(1)
	}
}

// voiceCmd is the voice frontend: record until Enter, transcribe locally, then
// hand the text to the exact flow a typed `ctl do` uses — daemon reasoning,
// plan preview, one confirmation, gated execution. Voice is only an input
// method; it earns no shortcut through the permission model.
func voiceCmd() {
	wav := filepath.Join(os.TempDir(), fmt.Sprintf("hyprvalet-voice-%d.wav", os.Getpid()))
	defer os.Remove(wav)

	stop, err := mic.Start(wav)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("listening — speak your request, then press Enter")
	_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	if err := stop(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	text, err := whisper.Default().Transcribe(ctx, wav)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if text == "" {
		fmt.Println("heard nothing — try again closer to the microphone")
		return
	}
	fmt.Printf("heard: %s\n", text)

	ctlPlan([]string{text}, true, tts.Default())
}

// logCmd shows the audit trail: the most recent attempted actions and what
// became of each, newest last. Default 20; `hyprvalet log 100` shows more.
func logCmd(rest []string) {
	n := 20
	if len(rest) > 0 {
		if _, err := fmt.Sscanf(rest[0], "%d", &n); err != nil || n <= 0 {
			fmt.Fprintf(os.Stderr, "usage: hyprvalet log [count]\n")
			os.Exit(2)
		}
	}
	list, err := events.Tail(n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading event log: %v\n", err)
		os.Exit(1)
	}
	if len(list) == 0 {
		fmt.Println("no events yet")
		return
	}
	for _, e := range list {
		detail := e.Detail
		if len(detail) > 60 {
			detail = detail[:57] + "..."
		}
		fmt.Printf("%s  %-6s  %-13s  %-28s %s  %s\n",
			e.At.Local().Format("2006-01-02 15:04:05"), e.Source, e.Kind, e.Cap, formatArgs(e.Args), detail)
	}
}

func pingCmd() {
	resp, err := daemon.Send(daemon.SocketPath(), protocol.Request{Op: protocol.OpPing})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if resp.Status != protocol.StatusPong {
		fmt.Fprintf(os.Stderr, "unexpected reply: %+v\n", resp)
		os.Exit(1)
	}
	fmt.Printf("daemon alive — %d capabilities\n", resp.Count)
}

func ctlCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hyprvalet ctl <ping|run> ...")
		os.Exit(2)
	}
	switch args[0] {
	case "ping":
		pingCmd()
	case "run":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: hyprvalet ctl run <capability> [key=value ...]")
			os.Exit(2)
		}
		ctlRun(args[1], args[2:])
	case "ask":
		ctlAsk(args[1:])
	case "plan":
		ctlPlan(args[1:], false, nil)
	case "do":
		ctlPlan(args[1:], true, nil)
	default:
		fmt.Fprintf(os.Stderr, "unknown ctl op %q (want ping|run|ask|plan|do)\n", args[0])
		os.Exit(2)
	}
}

// spokenPhrases are the few fixed sentences the voice frontend speaks, keyed by
// HYPRVALET_LANG. English is the default; the language should match the
// installed TTS voice (HYPRVALET_VOICE) — they are two halves of one choice.
var spokenPhrases = map[string]map[string]string{
	"English": {
		"done":    "Done.",
		"stopped": "I had to stop.",
		"denied":  "That is not permitted.",
		"nothing": "I found nothing I can do for that.",
	},
	"Spanish": {
		"done":    "Listo.",
		"stopped": "Tuve que detenerme.",
		"denied":  "Eso no está permitido.",
		"nothing": "No encontré nada que pueda hacer con eso.",
	},
}

// phrase resolves one fixed spoken sentence for the configured language,
// falling back to English for unknown languages or missing keys.
func phrase(key string) string {
	if set, ok := spokenPhrases[strings.TrimSpace(os.Getenv("HYPRVALET_LANG"))]; ok {
		if p := set[key]; p != "" {
			return p
		}
	}
	return spokenPhrases["English"][key]
}

// say speaks text when a speaker is present. Speech is an output garnish:
// failures warn and are swallowed, never changing what the command does.
func say(speaker *tts.Client, text string) {
	if speaker == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := speaker.Speak(ctx, text); err != nil {
		fmt.Fprintf(os.Stderr, "warning: speech failed: %v\n", err)
	}
}

// ctlAsk has the daemon reason a single intent from natural language, previews
// it, then runs it through the daemon's two-phase confirm flow — the reasoning
// lives in the resident process; the human prompt stays here in the client.
// When the daemon reports a retryable failure (the capability rejected the
// model's arguments), it re-asks once with the error as feedback.
func ctlAsk(rest []string) {
	request := strings.TrimSpace(strings.Join(rest, " "))
	if request == "" {
		fmt.Fprintln(os.Stderr, "usage: hyprvalet ctl ask \"<what you want>\"")
		os.Exit(2)
	}

	attempt := request
	for i := 1; ; i++ {
		if escalated(i) {
			fmt.Fprintln(os.Stderr, "escalating to the stronger model")
		}
		resp, err := daemon.AskViaDaemon(daemon.SocketPath(), attempt, escalated(i))
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if resp.Status == protocol.StatusError {
			fmt.Fprintf(os.Stderr, "error: %s\n", resp.Error)
			os.Exit(1)
		}
		if len(resp.Plan) == 0 {
			if resp.Reply != "" {
				fmt.Println(resp.Reply)
				return
			}
			fmt.Println("no match — the model found nothing it could do for that request")
			if resp.Reasoning != "" {
				fmt.Printf("  reasoning: %s\n", resp.Reasoning)
			}
			return
		}

		step := resp.Plan[0]
		fmt.Printf("understood: %s %s\n", step.Cap, formatArgs(core.Args(step.Args)))
		if resp.Reasoning != "" {
			fmt.Printf("  reasoning: %s\n", resp.Reasoning)
		}

		run, err := daemon.RunViaDaemon(daemon.SocketPath(), step.Cap, step.Args, func(reason string) bool {
			return promptYes(reason + " — proceed?")
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		if run.Status == protocol.StatusError && run.Retryable && i < maxInterpretAttempts {
			fmt.Fprintf(os.Stderr, "arguments rejected: %s — asking the model to correct\n", run.Error)
			attempt = correctiveRequest(request, fmt.Errorf("%s", run.Error))
			continue
		}
		reportRun(run)
		return
	}
}

// ctlPlan has the daemon reason a multi-step plan (already validated and bound to
// the policy). With execute=false ("ctl plan") it previews only; with
// execute=true ("ctl do") it refuses a plan that has a denied step, confirms the
// whole plan once, then runs each step through the daemon — reusing the per-step
// confirm flow, with the daemon re-checking each step for TOCTOU. A non-nil
// speaker voices the summary and the outcome (the voice frontend passes one).
func ctlPlan(rest []string, execute bool, speaker *tts.Client) {
	request := strings.TrimSpace(strings.Join(rest, " "))
	if request == "" {
		fmt.Fprintln(os.Stderr, "usage: hyprvalet ctl plan|do \"<what you want>\"")
		os.Exit(2)
	}
	resp, err := daemon.PlanViaDaemon(daemon.SocketPath(), request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	if resp.Status == protocol.StatusError {
		fmt.Fprintf(os.Stderr, "error: %s\n", resp.Error)
		os.Exit(1)
	}
	if len(resp.Plan) == 0 {
		// Conversation, not action: show and speak the answer. Words execute
		// nothing, so this path never touches the gate.
		if resp.Reply != "" {
			fmt.Println(resp.Reply)
			say(speaker, resp.Reply)
			return
		}
		fmt.Println("no plan — the model found nothing it could do for that request")
		say(speaker, phrase("nothing"))
		return
	}

	if resp.Summary != "" {
		fmt.Printf("plan: %s\n", resp.Summary)
		say(speaker, resp.Summary)
	}
	var blockers []string
	for i, s := range resp.Plan {
		fmt.Printf("  %d. %s %s  [%s]\n", i+1, s.Cap, formatArgs(core.Args(s.Args)), s.Decision)
		if s.Decision == core.DecisionDeny.String() {
			blockers = append(blockers,
				fmt.Sprintf("step %d (%s) is blocked — denied by policy or needs arming (try 'hyprvalet arm %s')", i+1, s.Cap, s.Cap))
		}
	}
	if len(blockers) > 0 {
		fmt.Fprintln(os.Stderr, "this plan cannot run:")
		for _, b := range blockers {
			fmt.Fprintf(os.Stderr, "  - %s\n", b)
		}
		say(speaker, phrase("denied"))
		os.Exit(1)
	}

	if !execute {
		return // `ctl plan` previews only
	}
	if !promptYes(fmt.Sprintf("execute this %d-step plan?", len(resp.Plan))) {
		fmt.Println("aborted")
		return
	}

	n := len(resp.Plan)
	for i, s := range resp.Plan {
		if i > 0 {
			time.Sleep(stepPause)
		}
		run, err := daemon.RunStep(daemon.SocketPath(), s.Cap, s.Args)
		if err != nil {
			fmt.Fprintf(os.Stderr, "plan aborted at step %d/%d (%s): %v\n", i+1, n, s.Cap, err)
			os.Exit(1)
		}
		switch run.Status {
		case protocol.StatusRan:
			fmt.Printf("  [%d/%d] %s\n", i+1, n, run.Text)
		case protocol.StatusDenied:
			fmt.Fprintf(os.Stderr, "plan aborted at step %d/%d (%s): no longer permitted (state changed since preview)\n", i+1, n, s.Cap)
			say(speaker, phrase("stopped"))
			os.Exit(1)
		case protocol.StatusNeedsConfirm:
			// The plan was approved as a whole, yet the daemon now wants a
			// confirmation: a doom-loop tripped mid-plan. Stop rather than hammer.
			fmt.Fprintf(os.Stderr, "plan aborted at step %d/%d (%s): %s\n", i+1, n, s.Cap, run.Text)
			say(speaker, phrase("stopped"))
			os.Exit(1)
		default:
			fmt.Fprintf(os.Stderr, "plan aborted at step %d/%d (%s): %s\n", i+1, n, s.Cap, run.Error)
			say(speaker, phrase("stopped"))
			os.Exit(1)
		}
	}
	fmt.Println("plan done")
	say(speaker, phrase("done"))
}

// reportRun prints the outcome of a single daemon run and exits nonzero on a
// denial or error, so ctl commands share one result-handling shape.
func reportRun(resp protocol.Response) {
	switch resp.Status {
	case protocol.StatusRan:
		if resp.Text != "" {
			fmt.Println(resp.Text)
		}
	case protocol.StatusDenied:
		fmt.Fprintf(os.Stderr, "denied: %s\n", resp.Text)
		os.Exit(1)
	case protocol.StatusError:
		fmt.Fprintf(os.Stderr, "error: %s\n", resp.Error)
		os.Exit(1)
	default:
		fmt.Printf("%s: %s\n", resp.Status, resp.Text)
	}
}

// ctlRun runs a capability through the daemon, prompting the human here (in the
// client) if the daemon reports the action needs confirmation.
func ctlRun(id string, rest []string) {
	args, err := parseArgs(rest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(2)
	}
	resp, err := daemon.RunViaDaemon(daemon.SocketPath(), id, args, func(reason string) bool {
		return promptYes(reason + " — proceed?")
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	reportRun(resp)
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

// promptYes asks a yes/no question, defaulting to no. Non-TTY stdin (EOF) reads
// as no, so the tool fails closed when it cannot ask.
func promptYes(question string) bool {
	fmt.Print(question + " [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}
