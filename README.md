# hyprvalet

**A typed, permission-gated voice assistant for controlling an
[Omarchy](https://omarchy.org/) / [Hyprland](https://hypr.land/) desktop —
a Jarvis done right.**

Say _"Jarvis"_ into the room and a conversation window opens. Ask it to switch
workspaces, open apps, set a reminder, search the web, or spin up a project and
open [Claude Code](https://claude.com/claude-code) in it — hands-free, in your
own language. It reasons with a large cloud model (with a local fallback),
speaks back in a natural voice, remembers the conversation, and you can talk
over it to interrupt.

And underneath all of it: **the model never runs a shell.** It can only invoke
**typed capabilities** from an explicit allowlist — each declaring what it
touches and how risky it is — and disruptive actions ask before they act. The
result is an assistant you can actually trust to touch a live desktop.

## Why this design

Most "Jarvis" projects wire a language model straight to a shell and hope for
the best. hyprvalet takes the opposite bet: **the gate is the safety boundary,
never the prompt.** Anything not registered as a capability is impossible — not
"hopefully blocked". A misheard command can't `rm -rf` your home directory,
because there is no capability that runs arbitrary commands. When speech
recognition once garbled a question into an action, the screen-lock capability
being confirm-tier caught it — the typed gate did its job.

## What it can do

A real spoken session:

```
you   → "Jarvis"
jarvis→ "¿En qué puedo ayudarle, señor?"          (a varied, spoken greeting)
you   → "abrí el navegador y cambiá al workspace 2"
jarvis→ [opens browser, switches workspace]  "Listo."
you   → "¿qué es un agujero negro?"
jarvis→ [answers from the model's knowledge, briefly]
you   → "recordame en 15 minutos sacar el café"
jarvis→ [schedules a spoken reminder]  "Listo."
you   → "creá un proyecto llamado tienda y abrí Claude ahí"
jarvis→ "¿Procedo?"  → you: "sí"  → [scaffolds the folder, opens Claude Code in tmux]
you   → "¿qué está haciendo Claude?"
jarvis→ [reads Claude's terminal and explains it in plain language]
you   → "decile que sí, que agregue el logout"
jarvis→ "¿Procedo?"  → you: "sí"  → [relays your words into Claude]
you   → "chau"
jarvis→ "Hasta luego."                             (or it bows out after a minute of silence)
```

Everything above is one binary. No shell strings, ever.

## Architecture

hyprvalet is a hexagonal (ports-and-adapters) system. The core knows nothing
about `hyprctl`, the `omarchy` CLI, Ollama, Groq, ElevenLabs, whisper, or tmux —
only about small interfaces. Every provider is an adapter at the edge.

```
  voice / text ─► reason ─────► [ typed Capability ] ─► gate ─► execute
   whisper STT    Groq (cloud)      registry          policy    hyprctl / omarchy /
   wake word    → Ollama (local)   (the allowlist)   allow·ask   tmux / xdg-open / …
   VAD, barge-in   fallback             │             ·deny
   ElevenLabs►Edge►piper TTS      AccessKind · Risk        arming · session grants
                                  (what)      (safe/confirm)   doom-loop breaker
                                        │
                                  append-only audit log  ·  episodic memory
```

Load-bearing ideas (grounded by studying five agent-orchestration projects — see
`docs/SOURCES.md`):

- **Typed capability registry as an allowlist.** Nothing outside it is
  reachable. Each capability validates its own arguments on every call and
  returns a *corrective error* — which the reasoning loop feeds back to the
  model for a retry — instead of executing on bad input.
- **Separate _what_ from _if_.** A capability's `AccessKind` (what it touches)
  is distinct from the decision of whether it runs. Safe actions run; Confirm
  actions ask first — by voice or keyboard, failing closed.
- **Resilient by composition.** Reasoning is Groq → local Ollama; voice is
  ElevenLabs → Edge → piper. Losing the network degrades quality, never
  availability. A cloud model that fails is announced, not silently swapped.
- **The agent reasons for you, but never consents for you.** The Claude Code
  bridge lets it read and relay, but you approve every action, and Claude's own
  permission prompts still stand.

## Capabilities (25)

| Domain | Capabilities |
|---|---|
| Workspaces / windows | `workspace.switch`, `window.move_to_workspace`, `window.close`, `window.fullscreen` |
| Apps & web | `app.open`, `browser.open`, `music.open`, `web.open`, `web.search` |
| Media & audio | `media.play_pause`, `media.next`, `media.previous`, `volume.set`, `volume.mute` |
| Desktop | `theme.next`, `theme.set`, `nightlight.toggle`, `screenshot.take`, `system.lock`, `omarchy.run` |
| Assistant | `reminder.set` (proactive spoken reminders) |
| Claude Code bridge | `project.new`, `project.open`, `terminal.read`, `terminal.send` |

Adding one is small: implement `core.Capability` in an adapter and register it.

## Quickstart

Requires [Go](https://go.dev/) 1.23+ and a running Hyprland session.

```bash
git clone https://github.com/xebastian153/hyprvalet.git
cd hyprvalet
go build -o hyprvalet ./cmd/hyprvalet

./hyprvalet list                                  # what it can do, and its policy
./hyprvalet workspace.switch workspace=3          # run a capability directly
./hyprvalet do "abrí el navegador y volvé al workspace 2"   # reason + confirm + run
```

Reasoning uses local Ollama out of the box (`HYPRVALET_MODEL`, default
`qwen2.5:7b`). Set `GROQ_API_KEY` to use a large cloud model
(`openai/gpt-oss-120b`) with the local model as an automatic fallback.

### Voice

```bash
./hyprvalet say "hola"        # speak text (needs a TTS backend: piper / edge-tts / ElevenLabs)
./hyprvalet voice             # a hands-free conversation window
./hyprvalet listen            # always-on: opens the window on the wake word ("jarvis")
```

For the full desktop experience — an always-on wake-word service and a
`SUPER+A` keybinding — see the example units in `configs/systemd/` and the
`configs/` directory (policy, recipes, echo-cancellation).

### Configuration

Everything is environment-driven; secrets live in a `0600` file read by the
systemd units. The main knobs:

| Variable | Purpose |
|---|---|
| `GROQ_API_KEY` / `HYPRVALET_GROQ_MODEL` | cloud reasoning (default `openai/gpt-oss-120b`) |
| `HYPRVALET_MODEL` | local Ollama model (fallback / offline) |
| `HYPRVALET_LANG` | spoken-output language (`English` / `Spanish`) |
| `ELEVENLABS_API_KEY` / `HYPRVALET_VOICE` | natural TTS voice (falls back to Edge, then piper) |
| `HYPRVALET_WHISPER_MODEL` / `HYPRVALET_STT_LANG` | speech recognition (whisper.cpp) |
| `HYPRVALET_WAKE_WORD` | wake word + comma-separated alternates |
| `HYPRVALET_BARGE_IN` | interrupt-while-speaking (needs headphones or echo cancellation) |
| `HYPRVALET_PROJECTS_DIR` | where `project.new` scaffolds (default `~/proyectos`) |

The permission policy is an installer-owned TOML at
`~/.config/hyprvalet/policy.toml` (see `configs/policy.example.toml`); a broken
policy fails closed.

## Project layout

```
cmd/hyprvalet/          CLI + voice frontend
internal/core/          domain: Capability, AccessKind, Risk, policy, audit, memory
internal/protocol/      typed daemon/client contract
internal/daemon/        resident actor-model daemon (Unix socket)
internal/adapters/
  hypr, omarchy, media, audio, web, remind, project, terminal   capabilities
  ollama, groq, fallback, prompt                                reasoning
  whisper, mic, tts, elevenlabs, edgetts, speech                voice
  policyfile, recipefile, eventlog                              persistence
docs/DESIGN.md          deep architecture   ·   docs/SOURCES.md   provenance
```

## Contributing

New capabilities are the easiest place to help: implement the `core.Capability`
interface in an adapter, validate your arguments (return a corrective error, not
a crash), and register it. Keep the core free of any dependency on a specific
tool — that separation is the whole point.

## License

[MIT](./LICENSE)
