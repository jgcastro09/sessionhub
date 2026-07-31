# Usage

Session Hub opens on the current session and terminal. The sidebar lists
sessions and live instances; the top bar shows session, workspace, active
Executor, Git branch, and process state. The status bar shows token totals,
estimated API-equivalent cost, duration, control owner, and key hints.

Core keys:

| Key | Action |
| --- | --- |
| `f12` | Leave terminal focus (the only key that does — everything else, including `esc`/`ctrl+g`/`ctrl+p`/`ctrl+b`/`ctrl+q`, is passed straight through to the focused CLI) |
| `f9` | Toggle voice dictation while terminal-focused: first press records from the mic, second press transcribes locally (Whisper) and pastes the text into the focused CLI |
| `enter` | Focus or activate the selected item |
| `tab` / `shift+tab` | Change Hub section |
| `ctrl+g` | Open Hub command mode (Hub mode only) |
| `ctrl+p` | Open command palette (Hub mode only) |
| `ctrl+b` | Toggle sidebar and resize PTY (Hub mode only) |
| `ctrl+f` | Toggle terminal focus layout and resize PTY (Hub mode only) |
| `ctrl+c` | Quit (Hub mode only); while terminal-focused it is passed to the CLI |
| `q` | Quit from Hub mode |

Command mode exposes Projects, Executors, Queues, Tasks, Registry, Pipelines,
Automations, Metrics, Logs, Remote, and Settings. Destructive actions
distinguish stopping a process, removing its session association, and
deleting persisted history. They require separate confirmation.

## Task Manager

The Tasks section lists the active project's cards (`← →` picks the
project). Keys: `n` new task, `enter` read the full card, `c` change status
(the workflow itself is validated by `internal/tasks`, not the UI — an
illegal transition just comes back as an error), `a` run the card's Audit
Contract and show the report, `/` filter by id/title. A task only ever
auto-completes from a passing Audit Contract, never from a status you set by
hand plus a guess.

The Web Panel's Tasks page (`/projects/:id/tasks`) adds what the TUI
deliberately doesn't: a drag-and-drop Kanban board, a full card editor
(sections, impacted areas, dependencies), a "Contexto técnico" panel that
resolves and lets you attach Registry references by searching, and a
read-only view of which Executor/terminal currently claims a task (that claim
is runtime-only — it's never written to `.shproject` or Git).

Scriptable equivalents: `sessionhub tasks list|create|show|status|search|audit`
(add `--json` for machine-readable output), run from anywhere inside a
project the same way `git` finds its repo.

## Code Registry

The Registry section shows the active project's coverage (does every scanned
file have an entry?) and lets you scan (`s`), read an entry (`enter`), filter
by path/module (`/`), or run a real semantic search over the filtered query
(`m` — "meaning"). Lexical search is instant and always available; semantic
search downloads and starts a small local embedding engine the first time
it's used (see below) and falls back to lexical automatically if that engine
isn't available yet.

The Web Panel's Registry page (`/projects/:id/registry`) adds a file Reader,
a review form for describing/classifying an entry (preserved across future
scans), and related-entries navigation.

Scriptable equivalents: `sessionhub registry scan|build|validate|search|context|review|git`.
`registry search --semantic` uses the same local embedding engine as the TUI
and Web Panel; `registry git` shows branch/upstream/ahead-behind/conflict
state correlated with which Registry entries the working tree currently
touches.

Semantic search is completely local and offline: the first use downloads a
self-contained copy of [llama.cpp](https://github.com/ggml-org/llama.cpp)
running in `--embedding` mode plus a small (~24MB) MiniLM embedding model
into `tools/` under Session Hub's data directory
(`~/.sessionhub/tools/llama/` and `~/.sessionhub/tools/embedding-models/`),
verified by a hardcoded checksum before use — the same pattern voice
dictation already uses for Whisper. It talks to itself over loopback only;
nothing is sent to any cloud API, and nothing outside `localhost` can reach
it.

## Factory Reset

The Settings tab has a Factory Reset that wipes the entire data directory —
every session, executor, login, log, and downloaded file — back to a clean
first-install state. It cannot be undone, so it's gated behind 3 steps:

1. `ctrl+r` in the Settings tab.
2. Confirm the y/n warning.
3. Type the exact phrase `DELETE EVERYTHING` and press `ctrl+s`.

Session Hub then quits; the next launch recreates the data directory from
scratch, exactly like a fresh install.

## Voice dictation

Click the `🎙 MICROFONE` button in the top-right corner (or press `f9`) while
a CLI tab is focused to start recording from the default microphone. The
button becomes `■ PARAR` while it is capturing; click it again (or press F9)
to stop and capture the final words. Text appears in that same tab
progressively, about every two seconds, as you speak.
Transcription is fully local and offline — the first press ever downloads a
self-contained copy of [whisper.cpp](https://github.com/ggml-org/whisper.cpp)
and the more accurate multilingual Whisper **small** model (~465MB, one-time)
into `tools/` under Session Hub's data directory
(`~/.sessionhub/tools/whisper-models/ggml-small.bin`).
The setup status shows each download's percentage and transferred megabytes;
afterward, the validated model stays there and is reused across app restarts
and tool upgrades. The transcription server runs in the background so later
dictations don't reload the model. Nothing is sent to any cloud API.

Supported on **Windows** (WASAPI, in-process) and **macOS** (a small
self-built native helper, since neither a pure-Go CoreAudio library nor an
official whisper.cpp macOS binary exists — Session Hub builds and ships
both itself). Linux isn't supported yet and shows a clear "not supported"
message instead of failing silently.

**On macOS**, the first time you use `f9` you'll need to grant microphone
access to whichever terminal app you're running Session Hub from (System
Settings → Privacy & Security → Microphone) — there's no way to trigger
that prompt from a plain command-line tool ahead of time.
