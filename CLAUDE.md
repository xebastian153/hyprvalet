# hyprvalet

A typed, permission-gated agent for controlling an Omarchy/Hyprland desktop with
natural language. The model never runs arbitrary shell — it invokes typed
capabilities from an explicit allowlist, and destructive actions require
confirmation.

**Read `HANDOFF.md` first.** It carries current state, next actions, and the
decisions worth not re-litigating. The deep architecture lives in `docs/DESIGN.md`;
the design's provenance — which decision came from which studied repo, with
`file:line` evidence and the exact commit analyzed — is in `docs/SOURCES.md`.

## Non-negotiables

- **Language: Go.** Single static binary, long-lived daemon, `goroutines`+channels
  as the actor model. Do not introduce another language for the core.
- **Hexagonal.** The core (`internal/core`) must never depend on `hyprctl`, the
  `omarchy` CLI, an LLM, or any concrete tool. Adapters (`internal/adapters/*`)
  implement the `core.Capability` port; the core knows only the interface.
- **Security by design.** The capability registry is an **allowlist** — anything
  not registered is impossible, not "hopefully blocked". Keep `AccessKind` (what
  an action touches) separate from the decision of whether it's allowed. Every
  capability validates its own args on every call and returns a corrective error
  instead of executing on bad input. Destructive actions are `RiskConfirm`.
- **The LLM never emits shell.** When the reasoning layer lands (M2+), it maps
  intent to a typed capability call with validated params — never a bash string.
- **License: MIT.** This is a public open-source repo. Keep it that way.
- **Commits: conventional, no AI attribution.** `feat|fix|chore|docs|refactor|…`.
  Never add `Co-Authored-By` or any AI-attribution trailer.
- **Artifacts in English.** Code, comments, docs, UI strings, commit messages —
  English, regardless of the conversation language.

## Architecture (summary — full version in `docs/DESIGN.md`)

```
  natural language ─► intent → plan ─► [ typed Capability ] ─► execute
   (CLI today,           (LLM: local        registry           hyprctl /
    voice later)      Ollama + escalate)  (the allowlist)      omarchy CLI
                                               │
                                     AccessKind · Risk tier
```

## Layout

```
cmd/hyprvalet/          CLI entry point
internal/core/          Capability, AccessKind, Risk, Registry (the allowlist)
internal/adapters/
  hypr/                 hyprctl-backed capabilities
  omarchy/              omarchy-CLI-backed capabilities
docs/DESIGN.md          deep architecture + the sources studied
HANDOFF.md              current state, next actions, decisions log
```

## Build & run

```bash
go build -o hyprvalet ./cmd/hyprvalet
./hyprvalet list
./hyprvalet workspace.switch workspace=3
```

Go is installed via `mise` (`go@1.26.5`).

## Engram

`.engram/config.json` scopes memory to project `hyprvalet`. It resolves only when
the session cwd is inside this directory — so this project's memory is only
reachable from a `claude` launched here (`cd ~/hyprvalet && claude`). Confirm with
`mem_current_project` before saving; it should report `hyprvalet`, not `ambiguous`.
Topic key for the project memory: `project/hyprvalet`.
