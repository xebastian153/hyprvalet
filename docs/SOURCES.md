# hyprvalet — Sources & Design Provenance

Where each design decision came from. hyprvalet's architecture was grounded by
studying five agent-orchestration projects. This file records **which source
drove which decision** and the concrete **file:line evidence** behind each
pattern, so you can trust — or re-derive — any claim without re-reading the repos.

## ⚠️ The clones are ephemeral — re-clone if you need the source

The repos were cloned into a **session-scoped scratchpad** during the original
research session and are **not** part of this project. They will be garbage-
collected and are not reachable from other sessions. Do **not** rely on any
`/tmp/claude-*/.../scratchpad/...` path.

If you need to read a source, re-clone it **at the pinned commit we analyzed** —
upstream may have changed, and a conclusion must never be carried across a version
boundary without re-deriving it against the exact tree.

```bash
# Example: re-clone grok-build at the analyzed commit
git clone https://github.com/xai-org/grok-build /tmp/grok-build
git -C /tmp/grok-build checkout 98c3b24
```

| Source | URL | Analyzed at (commit) | Lang |
|---|---|---|---|
| grok-build | https://github.com/xai-org/grok-build | `98c3b24` | Rust |
| opencode | https://github.com/anomalyco/opencode | `a46374e` | TS / Effect |
| t3code | https://github.com/pingdotgg/t3code | `7b8d126` | TS / Effect |
| hermes-agent | https://github.com/nousresearch/hermes-agent | `d9ee342` | Python |
| openclaw | https://github.com/openclaw/openclaw | `282879b3` | TS |

> Commit SHAs are short; `git checkout <sha>` resolves them. If a SHA no longer
> exists upstream (force-push, rebase), analyze the nearest tag and note the drift.

## Decision → provenance

| Decision (see `../HANDOFF.md`) | Driven by | Rationale |
|---|---|---|
| `AccessKind` (what) separate from `Decision` (if) | grok-build | Clean permission model; no `if cmd.contains("rm")` hacks |
| Typed capability registry as a strict **allowlist** | all five converge | The core safety property — unregistered = impossible |
| Validate args on every call → corrective error, not execution | opencode, grok-build | Reliability; the LLM can self-correct instead of misfiring |
| Daemon (headless) + thin clients over a typed protocol | all five converge | Clean UI/executor boundary; multi-frontend for free |
| Two-layer loop (actor mailbox + reason→tool→observe) | grok-build | Cancellation, mid-turn interjection, system-event handling |
| Doom-loop breaker | grok-build, opencode | Cut degenerate loops with real side effects |
| Event-sourcing (replay / undo / reconnect) | t3code | Undo on a live desktop is worth the structure |
| Temporal **"arming"** of dangerous capabilities | openclaw | Better than indefinite "remember for session" |
| Permission **scales with power** (read < write < admin) | openclaw | Not a binary allow/deny |
| **Plan-binding** against TOCTOU | openclaw | You approve X, X runs — not a mutated Y |
| Recipes + `lifecycle-guard` (refuse self-restart) | hermes-agent | M1 recipes must not `pkill`/restart their own host |
| Stack = **Go** (not Python) | hermes-agent's verdict | Python paid a heavy price for a long-lived daemon |

## Per-source: what we took, with evidence

Paths below are relative to each repo's root at the pinned commit.

### grok-build (Rust) — `98c3b24`

- **`AccessKind` vs `Decision`** — `crates/codegen/xai-grok-workspace/src/permission/types.rs:145` (`AccessKind`: Read/Edit/Bash/…), `:164` (`Decision`: Allow/Ask/Reject/PolicyDeny). Each tool call maps to an `AccessKind` at `.../session/acp_session_impl/tool_calls.rs:893`.
- **Schema auto-derived from the Rust type** (never hand-written) — trait `Tool` at `crates/common/xai-tool-runtime/src/tool.rs:36`; `input_schema: generate_schema::<T::Args>()` at `crates/codegen/xai-grok-tools/src/registry/types.rs:595`.
- **Two-layer loop** — actor mailbox (`tokio::select!` over commands+timers) at `.../session/acp_session_impl/run_loop.rs:153`; agentic round loop (reason→tool→observe) at `.../turn.rs:759`.
- **Permission modes** Ask / Auto (cheap LLM classifier) / Yolo — `crates/codegen/xai-grok-workspace/src/permission/manager.rs`.
- **Doom-loop detection** — `crates/codegen/xai-grok-sampler/src/doom_loop.rs`.
- **Leader/follower daemon** over a Unix socket (`~/.grok/leader.sock`) — `.../leader/mod.rs`; `WorkspaceOps: Local | Proxy` at `.../workspace_ops.rs:1467`.
- **OS sandbox per subprocess** (Landlock/seccomp) — crate `xai-grok-sandbox`.

### opencode (TS / Effect) — `a46374e`

- **Validate-on-every-call → model-facing corrective error** — `packages/opencode/src/tool/tool.ts:99-148` (`wrap`), `InvalidArgumentsError` at `:24-34`.
- **Tool registry / allowlist** — `packages/opencode/src/tool/registry.ts:204-247`; per-model/agent filtering at `:286-335`.
- **Permission service** (blocks until a human replies; `once`/`always`/`deny`) — `packages/opencode/src/permission/index.ts` (`evaluate` `:28-38`, `ask` `:67-107`, `reply` `:109-167`).
- **Doom-loop detector** — `packages/opencode/src/session/processor.ts:356-380`.
- **Server-core + SSE, thin clients** — `packages/opencode/src/server/server.ts`; event stream `.../server/routes/instance/httpapi/groups/event.ts`.

### t3code (TS / Effect) — `7b8d126`

- **Single typed contract package** (shared by daemon + clients) — `packages/contracts/src/orchestration.ts:682-701` (dispatchable commands), `packages/contracts/src/rpc.ts`.
- **CQRS / event-sourcing as the "agent loop"** — `apps/server/src/orchestration/decider.ts`; reactor `.../Layers/ProviderCommandReactor.ts`; engine `.../OrchestrationEngine.ts:49`.
- **Two-level approval** `accept | acceptForSession | decline | cancel` — `packages/contracts/src/orchestration.ts:131`; granular request types `packages/contracts/src/providerRuntime.ts:136-140`.
- **Daemon + thin clients + relay + reconnect supervisor** — `packages/client-runtime/src/connection/supervisor.ts:31` (backoff), `.../relay/managedRelay.ts` (NAT-traversal + phone push).
- **Driver SPI** (plain value, config decoded once, Scope-managed lifecycle) — `apps/server/src/provider/ProviderDriver.ts:113`.

### hermes-agent (Python) — `d9ee342`

- **Two-axis cron** (trigger separate from execution+delivery) — `cron/scheduler_provider.py:26-100` (pluggable `CronScheduler` ABC); default in-process loop `:170-230`.
- **Typed blueprints** ("users never write raw cron") — `cron/blueprint_catalog.py:1-25`.
- **Lifecycle-guard** (reject a job that would restart/kill the host) — `cron/lifecycle_guard.py:1-40`.
- **Tools-via-RPC from a generated script** (collapse multi-step into one turn) — `tools/code_execution_tool.py:1-17`.
- **Isolated subagents** — `tools/delegate_tool.py:1-17`.
- **Abstract execution backend** (local/docker/ssh/singularity/modal/daytona) — `tools/environments/`.
- **ACP bidirectional** — server `acp_adapter/server.py`, client `agent/copilot_acp_client.py`; installable-agent manifest `acp_registry/agent.json`.
- *Verdict:* excellent design, but the Python daemon shows the cost — 1 MB+ source files (`gateway/run.py`, `cli.py`), manual GIL/threads/asyncio scaffolding. This is why hyprvalet is Go.

### openclaw (TS) — `282879b3`

- **Gateway (control plane) ↔ Node (device declaring a typed command surface)** — `docs/nodes/index.md`; per-platform command allowlist `src/gateway/node-command-policy.ts:1-33`; `dangerous` flags on commands `:22-33`.
- **Temporal "arming"** of dangerous capability groups (auto-expiry) — `extensions/phone-control/index.ts:72-77`.
- **Permission scales with action power** (`operator.pairing` < `+write` < `+admin`) — `docs/nodes/index.md`.
- **Plan-binding against TOCTOU** (freeze canonical plan before approval; re-validate) — `src/node-host/exec-policy.ts`, `src/infra/exec-approvals.ts:52`.
- **Declarative capability manifest** (`activation` triggers, `configSchema` + `uiHints`) — `extensions/*/openclaw.plugin.json`; typed `register*` surface `src/plugins/api-builder.ts`.
- *Explicit non-goal we adopt:* no nested agent hierarchies (planner-of-planners) — keep the core thin, capabilities as adapters (`VISION.md`).
