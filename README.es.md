<div align="center">

# 🎙️ hyprvalet

**Un asistente de voz tipado y con permisos para
[Omarchy](https://omarchy.org/) / [Hyprland](https://hypr.land/) — un Jarvis bien hecho.**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Platform](https://img.shields.io/badge/platform-Hyprland%20·%20Omarchy-1793D1)](https://hypr.land/)
[![Architecture](https://img.shields.io/badge/arquitectura-hexagonal-blueviolet)](./docs/DESIGN.md)

[English](./README.md) · **Español**

</div>

---

Dices _"Jarvis"_ y se abre una ventana de conversación. Le pides que cambie de
workspace, abra aplicaciones, programe un recordatorio, busque en internet, o que
cree un proyecto y abra [Claude Code](https://claude.com/claude-code) en él —
manos libres, en tu idioma. Razona con un modelo grande en la nube (con respaldo
local), responde con voz natural, recuerda la conversación, y puedes
interrumpirlo hablando sobre él.

Y debajo de todo: **el modelo nunca ejecuta un shell.** Solo puede invocar
**capabilities tipadas** de un allowlist explícito — cada una declara qué toca y
qué tan riesgosa es — y las acciones disruptivas preguntan antes de actuar.

## Por qué este diseño

La mayoría de los proyectos "Jarvis" conectan un modelo de lenguaje directo a un
shell y confían en la suerte. hyprvalet apuesta lo contrario: **el gate es la
frontera de seguridad, nunca el prompt.** Lo que no está registrado como
capability es imposible — no "ojalá bloqueado". Un comando mal entendido no puede
ejecutar `rm -rf` sobre tu carpeta personal, porque ninguna capability corre
comandos arbitrarios. Una vez el reconocimiento de voz confundió una pregunta con
una acción, y la capability de bloqueo de pantalla —al ser de confirmación— lo
detuvo: el gate tipado hizo su trabajo.

## Cómo funciona

Una solicitud hablada atraviesa cuatro etapas. El razonamiento mapea la
intención a una **llamada a una capability tipada** —nunca un string de shell— y
el gate de permisos, no el prompt, decide si se ejecuta.

```mermaid
flowchart LR
    subgraph FE["Frontend · borde"]
        direction TB
        WW["Palabra de activación + VAD<br/>interrupción"] --> STT["Voz a texto<br/>whisper.cpp"]
    end

    subgraph BR["Razonamiento · puertos"]
        direction TB
        GQ["Groq<br/>gpt-oss-120b"] -.-> OL["Ollama<br/>local · respaldo"]
    end

    subgraph GT["Gate de permisos · core"]
        direction TB
        PO["Política<br/>permitir · preguntar · negar"] --- AR["Armado · sesión<br/>corta-bucles"]
    end

    subgraph EX["Capability · allowlist"]
        direction TB
        CA["hyprctl · omarchy<br/>tmux · web · …"]
    end

    STT -->|lenguaje natural| BR
    BR -->|intención tipada| GT
    GT -->|permitido| EX
    GT -.-> TTS
    EX --> TTS["Texto a voz<br/>ElevenLabs → Edge → piper"]
```

## Arquitectura

hyprvalet es un sistema **hexagonal** (puertos y adaptadores). El core depende
solo de interfaces pequeñas — no conoce `hyprctl`, el CLI de `omarchy`, Ollama,
Groq, ElevenLabs, whisper ni tmux. Cada herramienta concreta es un adaptador en
el borde, intercambiable sin tocar el core.

```mermaid
flowchart TB
    subgraph ADAPT_IN["Adaptadores de entrada"]
        direction LR
        CLI["CLI"]
        VOICE["Frontend de voz<br/>whisper · mic · TTS"]
        DAEMON["Daemon residente<br/>socket Unix"]
    end

    subgraph CORE["core — dominio puro, sin dependencias de herramientas"]
        direction TB
        REG["Registro<br/>el allowlist"]
        CAP["Capability · AccessKind · Risk"]
        POL["Política · Armado · Sesión"]
        AUD["Registro de auditoría · Memoria episódica"]
    end

    subgraph PORTS["Puertos"]
        direction LR
        LP["LLMPort · PlannerPort"]
        CP["Puerto Capability"]
    end

    subgraph ADAPT_OUT["Adaptadores de salida"]
        direction TB
        REASON["ollama · groq · fallback"]
        DESKTOP["hypr · omarchy · media · audio · web"]
        BRIDGE["project · terminal<br/>puente con Claude Code"]
    end

    CLI --> CORE
    VOICE --> CORE
    DAEMON --> CORE
    CORE --> PORTS
    LP --> REASON
    CP --> DESKTOP
    CP --> BRIDGE
```

Ideas que sostienen el diseño (fundadas al estudiar cinco proyectos de
orquestación de agentes — ver [`docs/SOURCES.md`](./docs/SOURCES.md)):

- **El registro de capabilities tipadas es un allowlist.** Nada fuera de él es
  alcanzable. Cada capability valida sus propios argumentos y devuelve un *error
  correctivo* — que el bucle de razonamiento reinyecta al modelo para
  reintentar — en vez de ejecutar con datos inválidos.
- **Separar el _qué_ del _si_.** El `AccessKind` de una capability (qué toca) es
  distinto de la decisión de si se ejecuta. Lo Safe corre; lo Confirm pregunta
  primero — por voz o teclado, fallando cerrado.
- **Resiliente por composición.** El razonamiento es Groq → Ollama local; la voz
  es ElevenLabs → Edge → piper. Perder la red degrada la calidad, nunca la
  disponibilidad — y la degradación se anuncia, nunca es silenciosa.
- **Razona por ti, jamás consiente por ti.** Ver el puente con Claude Code.

## El puente con Claude Code

hyprvalet puede abrir [Claude Code](https://claude.com/claude-code) en un
proyecto, **leer** lo que muestra, y **retransmitir** por voz tus respuestas —
pero tú apruebas cada acción, y los propios permisos de Claude siguen en pie. El
asistente conversa en tu nombre; nunca consiente en tu nombre.

```mermaid
sequenceDiagram
    actor Usuario
    participant J as Jarvis
    participant C as Claude Code
    Usuario->>J: "crea un proyecto llamado tienda"
    J->>Usuario: "¿procedo?"
    Usuario->>J: "sí"
    J->>C: crea la carpeta + abre en tmux
    Usuario->>J: "¿qué está haciendo Claude?"
    J->>C: lee la terminal (tmux capture-pane)
    C-->>J: pantalla en vivo
    J-->>Usuario: resumen hablado en lenguaje natural
    Usuario->>J: "dile que sí, que agregue el logout"
    J->>Usuario: "¿procedo?"
    Usuario->>J: "sí"
    J->>C: retransmite (tmux send-keys)
    Note over Usuario,C: Tú sigues en el bucle en cada acción.
```

## Capabilities (28)

| Dominio | Capabilities |
|---|---|
| Workspaces / ventanas | `workspace.switch` · `window.move_to_workspace` · `window.close` · `window.fullscreen` |
| Aplicaciones y web | `app.open` · `browser.open` · `music.open` · `web.open` · `web.search` |
| Multimedia y audio | `media.play_pause` · `media.next` · `media.previous` · `volume.set` · `volume.mute` |
| Escritorio | `theme.next` · `theme.set` · `nightlight.toggle` · `screenshot.take` · `system.lock` · `omarchy.run` |
| Asistente | `reminder.set` — recordatorios hablados proactivos · `memory.remember` · `memory.recall` · `memory.forget` |
| Puente con Claude Code | `project.new` · `project.open` · `terminal.read` · `terminal.send` |

Agregar una es simple: implementa la interfaz `core.Capability` en un adaptador y
regístrala.

## Inicio rápido

Requiere [Go](https://go.dev/) 1.23+ y una sesión de Hyprland en ejecución.

```bash
git clone https://github.com/xebastian153/hyprvalet.git
cd hyprvalet
go build -o hyprvalet ./cmd/hyprvalet

./hyprvalet list                                     # qué puede hacer, y su política
./hyprvalet workspace.switch workspace=3             # correr una capability directamente
./hyprvalet do "abre el navegador y vuelve al workspace 2"   # razonar → confirmar → correr
```

El razonamiento usa Ollama local de forma predeterminada (`HYPRVALET_MODEL`, por
defecto `qwen2.5:7b`). Define `OPENAI_API_KEY` o `GROQ_API_KEY` para usar un
modelo en la nube; los proveedores se componen como una cadena resiliente,
OpenAI → Groq → Ollama, así perder uno cae al siguiente. (Las suscripciones de
consumidor como ChatGPT Plus o Gemini Advanced no son acceso por API; hyprvalet
usa claves de API por uso, que para comandos hablados cortos cuestan centavos
al mes.)

### Voz

```bash
./hyprvalet say "hola"        # decir texto (necesita un backend TTS: piper / edge-tts / ElevenLabs)
./hyprvalet voice             # una ventana de conversación manos libres
./hyprvalet listen            # siempre activo: abre la ventana con la palabra "jarvis"
```

Para la experiencia completa de escritorio — un servicio de palabra de activación
siempre activo y un atajo `SUPER+A` — consulta las unidades de ejemplo en
[`configs/systemd/`](./configs/systemd/) y el directorio
[`configs/`](./configs/) (política, recetas, cancelación de eco).

### Configuración

Todo se controla por entorno; los secretos viven en un archivo `0600` que leen
las unidades systemd.

| Variable | Para qué |
|---|---|
| `OPENAI_API_KEY` · `HYPRVALET_OPENAI_MODEL` | razonamiento en la nube vía OpenAI — por defecto `gpt-4o-mini` (barato; centavos al mes para comandos cortos) |
| `GROQ_API_KEY` · `HYPRVALET_GROQ_MODEL` | razonamiento en la nube vía Groq — por defecto `openai/gpt-oss-120b` |
| `HYPRVALET_MODEL` | modelo Ollama local — respaldo / sin conexión |
| `HYPRVALET_LANG` | idioma de la salida hablada — `English` / `Spanish` |
| `ELEVENLABS_API_KEY` · `HYPRVALET_VOICE` | voz TTS natural — cae a Edge, luego piper |
| `HYPRVALET_WHISPER_MODEL` · `HYPRVALET_STT_LANG` | reconocimiento de voz — whisper.cpp |
| `HYPRVALET_WHISPER_GPU` | fija whisper a un dispositivo Vulkan (p. ej. una iGPU) para que no compita con el modelo de razonamiento; cae a CPU si falla |
| `HYPRVALET_WAKE_WORD` | palabra de activación + alternativas separadas por coma |
| `HYPRVALET_BARGE_IN` | interrumpir mientras habla — requiere auriculares o cancelación de eco |
| `HYPRVALET_PROJECTS_DIR` | dónde crea `project.new` — por defecto `~/proyectos` |
| `HYPRVALET_MEMORY` | backend de memoria de largo plazo — `engram` (aislado, por defecto si está instalado) / `jsonl` |

La política de permisos es un TOML propiedad del instalador en
`~/.config/hyprvalet/policy.toml` (ver
[`configs/policy.example.toml`](./configs/policy.example.toml)); una política
inválida falla cerrada.

## Estructura del proyecto

```
cmd/hyprvalet/          CLI + frontend de voz
internal/core/          dominio: Capability, AccessKind, Risk, política, auditoría, memoria
internal/protocol/      contrato tipado daemon/cliente
internal/daemon/        daemon residente modelo-actor (socket Unix)
internal/adapters/
  hypr · omarchy · media · audio · web · remind · project · terminal   capabilities
  ollama · groq · fallback · prompt                                    razonamiento
  whisper · mic · tts · elevenlabs · edgetts · speech                  voz
  policyfile · recipefile · eventlog                                   persistencia
docs/DESIGN.md          arquitectura profunda   ·   docs/SOURCES.md   procedencia
```

## Contribuir

Las capabilities nuevas son el lugar más fácil para ayudar: implementa la
interfaz `core.Capability` en un adaptador, valida tus argumentos (devuelve un
error correctivo, no un crash), y regístrala. Mantén el core libre de cualquier
dependencia de una herramienta concreta — esa separación es todo el punto.

## Licencia

[MIT](./LICENSE)
