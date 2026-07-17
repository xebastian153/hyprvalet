# hyprvalet

**A typed, permission-gated agent for controlling an [Omarchy](https://omarchy.org/) / [Hyprland](https://hypr.land/) desktop with natural language — done right.**

> Status: **early (M0)**. The foundation works today; the language layer is on the roadmap below.

Most "Jarvis" projects wire a language model straight to a shell and hope for
the best. hyprvalet takes the opposite approach: the model never runs arbitrary
commands. It can only invoke **typed capabilities** from an explicit allowlist —
each one declaring what it touches and how risky it is — and destructive actions
require confirmation. The result is an assistant you can actually trust to touch
a live desktop.

## Why

On Omarchy, everything is a keyboard shortcut. That's powerful, but you can't
remember a hundred of them, and shortcuts can't express *"set up my work
environment"* or *"move whatever I'm looking at to workspace 3"*. hyprvalet adds
a natural-language layer over the shortcuts you already have — without giving up
control over what actually runs.

## Design

hyprvalet is a hexagonal (ports-and-adapters) system. The core knows nothing
about `hyprctl` or the `omarchy` CLI — only about a `Capability` interface.

```
  natural language  ──►  intent → plan  ──►  [ typed Capability ]  ──►  execute
   (CLI today,               (LLM,                registry             hyprctl /
    voice later)          local + escalate)    (the allowlist)       omarchy CLI
                                                     │
                                          AccessKind  ·  Risk tier
                                        (what it touches) (safe/confirm)
```

Load-bearing ideas, drawn from studying five agent-orchestration projects:

- **Typed capability registry as an allowlist.** Anything not registered is
  impossible — not "hopefully blocked". Each capability validates its own
  arguments on every call and returns a corrective error instead of executing on
  bad input.
- **Separate *what* from *if*.** A capability's `AccessKind` (what it touches)
  is kept distinct from the decision of whether it's allowed. Safe actions run;
  Confirm actions ask first.
- **Pluggable brain.** The reasoning engine sits behind a port. The default is a
  **local model (Ollama)** — free, offline, private, and good enough because the
  model's job is narrow (map intent → one typed capability). Hard multi-step
  planning can escalate to a stronger model. Your desktop, your rules.

## Roadmap

| Milestone | What | Status |
|-----------|------|--------|
| **M0** | Capability registry + adapters (hyprctl, omarchy) + direct CLI, no LLM | ✅ in progress |
| **M1** | Deterministic recipes ("set up my work environment") bound to a keybind | ▫ planned |
| **M2** | Local-LLM intent layer: natural language → one typed capability | ▫ planned |
| **M3** | Multi-step planner with plan preview, temporal "arming", and confirmation | ▫ planned |
| **M4** | Long-lived daemon + frontends (voice, phone) | ▫ planned |

## Quickstart (M0)

Requires [Go](https://go.dev/) 1.23+, and a running Hyprland session with
`hyprctl` (and `omarchy` for the omarchy capability).

```bash
git clone https://github.com/SebasDevMag/hyprvalet.git
cd hyprvalet
go build -o hyprvalet ./cmd/hyprvalet

# List what the agent can do
./hyprvalet list

# Run capabilities directly (no LLM yet)
./hyprvalet workspace.switch workspace=3
./hyprvalet window.move_to_workspace workspace=2
./hyprvalet app.open cmd=firefox
./hyprvalet omarchy.run args="restart waybar"   # Confirm-tier: prompts first
```

## Project layout

```
cmd/hyprvalet/          CLI entry point
internal/core/          domain: Capability, AccessKind, Risk, Registry
internal/adapters/
  hypr/                 hyprctl-backed capabilities
  omarchy/              omarchy-CLI-backed capabilities
```

## Contributing

Early days — issues and ideas welcome. New capabilities are the easiest place to
help: implement the `core.Capability` interface in an adapter and register it.
Keep the core free of any dependency on a specific tool.

## License

[MIT](./LICENSE) © SebasDevMag
