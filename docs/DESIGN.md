# hyprvalet — Design

The architecture and the reasoning behind it. For current state and next actions,
see `../HANDOFF.md`.

## The problem with "Jarvis" projects

The common failure mode is wiring a language model straight to a shell: *"a mic +
an LLM that runs bash."* That is:

1. **Unsafe** — an LLM emitting arbitrary commands on a live machine is a time bomb.
2. **Unreliable** — it hallucinates flags, invents commands, uses stale syntax.
3. **Meaningless** — it doesn't know *what* it can actually do on this machine.

On Omarchy the "body" is already solved: `hyprctl` (Hyprland's IPC), the `omarchy`
CLI, `dbus`, `wtype`. The muscle exists and is scriptable. What's missing isn't
strength — it's the **brain** and, above all, the **contract** between brain and
muscle.

## The hexagon

```
  Frontends (input adapters):  CLI/keybind · walker · voice · phone (relay)
        │  typed protocol (RPC + event stream)
        ▼
  ┌───────────────── CORE (pure domain) ──────────────────────┐
  │  Intent Router → Planner → Executor                        │
  │  (knows nothing about hyprctl — only about Capabilities)   │
  └── LLMPort ─── Capability registry ─── Permission gate ─────┘
         │              │                       │
      Ollama /      the allowlist          AccessKind (what)
      escalate    (typed actions)         + Decision (if allowed)
                       │
        adapters: hyprctl · omarchy CLI · dbus · shell (bounded)
```

The core depends only on the `Capability` interface. Everything concrete —
`hyprctl`, `omarchy`, an LLM — is an adapter at the edge.

## The heart: the capability registry

The LLM never runs shell. Capabilities are typed, named, risk-tagged actions:

```
Capability {
  ID()          e.g. "window.move_to_workspace"
  Access()      AccessKind — what it touches (window/workspace/app/command)
  Risk()        safe | confirm | forbidden
  Params()      accepted parameters
  Run(ctx, args) validates args on every call, then executes
}
```

Why this changes everything:

- **Allowlist, not blocklist.** Whatever isn't registered is *impossible*, not
  "hopefully caught by a filter". A mistaken `rm` cannot happen because it isn't a
  capability.
- **Reliability.** Args are validated against the capability before anything runs;
  invalid input returns a corrective message the caller (human or LLM) can fix.
- **Safety by design.** `AccessKind` (what an action touches) is separate from the
  `Decision` (whether it's allowed). Destructive actions are `confirm`-tier.

## The safety model (the pro-vs-novice line)

Composed from the best of the five studied projects:

| Idea | From | What it buys |
|---|---|---|
| `AccessKind` (what) separate from `Decision` (if) | grok-build | No `if cmd.contains("rm")` hacks |
| Permission **scales with power**: read < write < admin | openclaw | Not a binary allow/deny |
| **Temporal "arming"**: dangerous caps OFF by default, armed for N minutes with auto-expiry | openclaw | Better than an indefinite "remember for this session" |
| **Plan-binding** against TOCTOU: freeze the canonical plan before approval, execute exactly that, re-validate world state | openclaw | You approve X, X runs — not a mutated Y |
| Auto tier = a cheap LLM classifier approves non-destructive actions | grok-build | Reversible actions flow without friction |
| **Doom-loop breaker**: repeat the same action N times → force confirmation | grok-build + opencode | Cuts degenerate loops with real side effects |

## The hybrid brain

Not everything goes through the LLM.

- **Recipes (named macros)** for the 20% you do daily ("set up my work
  environment"): deterministic, instant, free, 100% reliable. Defined once (M1).
- **LLM planner** for the long tail — the unpredictable requests (M2/M3).

The reasoning engine sits behind `LLMPort`:

- **Default: local (Ollama).** Free, offline, private — and *sufficient*, because
  the typed-action allowlist makes the model's job narrow (classify intent → pick
  one capability + fill params). A 7–14B local model handles that.
- **Escalate** to a stronger model only for hard multi-step planning.

Consumer chat subscriptions (Claude Pro/Max, ChatGPT Plus) are **not**
programmatically callable in a supported way. The only subscription-based path is
wrapping an agent CLI (e.g. Claude Code Max) in headless/ACP mode — a ToS gray
area, verify the plan's terms. The API with a cheap model + prompt caching would be
~cents/day for this bounded task; local-first was chosen for zero marginal cost and
privacy.

## Build order (each milestone is independently useful)

| Milestone | What | Why |
|---|---|---|
| **M0** ✅ | Registry + adapters (hyprctl, omarchy) + direct CLI, no LLM | Prove muscle + contract in isolation |
| **M1** | Recipe engine → "work environment" as a deterministic recipe on a keybind | Immediate utility, zero LLM |
| **M2** | Local-LLM intent layer: NL → one capability, safe-tier only | Add the brain with a safety net |
| **M3** | Multi-step planner + plan preview + arming + confirmation | Autonomy actually lands here |
| **M4** | Long-lived daemon + frontends (voice, phone) | The "Jarvis feel" on a proven base |

You never build the fragile part (autonomy) on unproven ground. By M3 you trust
every capability because you broke them in M0.

## Convergent patterns across the five sources

Five teams, different languages, no shared code — and they converge on the same
load-bearing patterns. When that happens, it's structure, not style:

1. **Daemon (headless) + thin clients over a typed protocol.** (grok: leader/follower
   over a Unix socket; opencode: HTTP + SSE; t3code: WS-RPC + relay.)
2. **Typed action, schema-derived, validated on every call**; invalid input →
   corrective error, not execution.
3. **Permission as a blocking, human-in-the-loop gate** with "remember for session",
   separating *what an action touches* from *whether it's allowed*.
4. **Doom-loop detection.**
5. **Event-sourced / normalized event stream** between reasoning and execution →
   replay, undo, audit, reconnect for free.
6. **Two-layer loop**: an actor mailbox (commands + timers + system events) on the
   outside, a reason→tool→observe round loop on the inside.

New ideas layered on top: two-axis scheduling + typed blueprints and a
self-restart lifecycle-guard (hermes); the Gateway↔Node split, temporal arming,
and plan-binding (openclaw); tools-via-RPC to collapse a multi-step chain into one
turn (hermes).

What to explicitly avoid (openclaw's own rule): nested agent hierarchies
(planner-of-planners). Keep the core thin; capabilities are plugins/adapters.
