# Architecture

## Boundaries

Session Hub is one foreground process. The Bubble Tea program is the host and
owns application lifetime. It composes services through narrow interfaces; the
TUI does not execute SQL, parse automation rules, or create operating-system
processes directly.

```text
TUI ──commands/events── Application services
 │                         ├── Executor manager ── PTY ── external process
 │                         │        └── VT emulator (display snapshot)
 │                         ├── Session/context/checkpoint services
 │                         ├── Automation engine/scheduler/watchers
 │                         ├── Metrics and Git state
 │                         └── Tailscale-only remote protocol
 └────render models──────────────┬───────────────────────────────
                                SQLite store
```

Raw PTY bytes have two independent consumers. The VT emulator consumes an
unaltered copy to produce the visible screen; persistence, recognition rules,
and metrics consume another copy asynchronously. Derived processing never
rewrites bytes displayed to the operator.

## Packages

- `cmd/sessionhub`: process entry point and version injection.
- `internal/app`: lifetime, recovery, and service composition.
- `internal/domain`: durable data structures and explicit state machines.
- `internal/store`: SQLite migrations and transactional repositories.
- `internal/terminal`: PTY process lifecycle, VT state, input ownership, and
  terminal snapshots.
- `internal/executor`: manual configuration validation and instance service.
- `internal/automation`: queues, schedules, pipelines, retries, approvals,
  budgets, watchers, deterministic commands, and workspace locks.
- `internal/context`: continuity packages and checkpoints.
- `internal/metrics`: token/cost calculation with precision labels.
- `internal/gitstate`: optional Git workspace inspection, including upstream
  tracking, ahead/behind counts, and merge-conflict detection.
- `internal/remote`: framed Tailscale host/client protocol.
- `internal/update`: GitHub release lookup and checksum-verified replacement
  preparation.
- `internal/ui`: Bubble Tea models and forms.
- `internal/project`: the portable `.shproject` manifest, the local project
  catalogue, and the `ResolvePath` containment check every `.shproject`-owning
  service (tasks, registry) reuses.
- `internal/tasks`: Task Manager — Markdown cards under
  `.shproject/tasks/cards`, workflow validation, and the Audit Contract
  runner. Depends on `internal/registry` only through the narrow
  `RegistryChecker`/`RecipeRunner` interfaces it declares, not the other way
  around.
- `internal/registry`: Code Registry — the file scanner, JSON records under
  `.shproject/registry/records`, lexical search, Git correlation, and the
  semantic index (embeddings cached on each entry, computed through the
  narrow `Embedder` interface `internal/embedding` implements).
- `internal/embedding`: self-installed, checksum-verified local semantic
  search engine (a llama.cpp server on loopback, `--embedding` mode).
- `internal/events`: the project-scoped publish/subscribe bus Task
  Manager/Code Registry writes go through, consumed by the Web Panel's SSE
  stream and the TUI's reload.
- `internal/atomicfile`, `internal/download`, `internal/procserver`: shared
  primitives — atomic temp-file-then-rename writes; checksum-verified HTTP
  downloads plus tar.gz/zip extraction; and local-subprocess-with-HTTP-API
  lifecycle helpers (free port, wait-until-healthy). `internal/voice` and
  `internal/embedding` both build on all three instead of each keeping its
  own copy.
- `internal/webserver`: the Web Panel's HTTP API and embedded SPA build.

## State and idempotency

Executor, queue item, step, pipeline, schedule, and approval states are finite
state machines validated before persistence. External effects use a durable
idempotency key. An automation transaction claims a pending record before
sending PTY input or starting a command; completion is then recorded with its
effect key. Recovery marks formerly running process-backed work interrupted,
but never replays completed effects.

SQLite runs in WAL mode with foreign keys and a busy timeout. Migrations are
embedded, ordered, and applied in `BEGIN IMMEDIATE` transactions. Structured
fields are encoded as JSON so schema evolution remains explicit while durable
identity, state, timestamps, and idempotency keys remain queryable.

## Platform PTY behavior

`go-pty` selects Unix PTY APIs on macOS/Linux and ConPTY on supported Windows.
Resize maps to `TIOCSWINSZ` plus `SIGWINCH` on Unix and
`ResizePseudoConsole` on Windows. Shutdown first requests a graceful process
exit, then closes the PTY and force-terminates the remaining process tree after
the configured grace period.

The embedded VT emulator, not the outer terminal, owns nested alternate-screen
and cursor state. The renderer produces an ANSI snapshot for Bubble Tea. Input
is serialized through a lease: local operator, automation, or one remote
controller may own writes, never more than one simultaneously.

## Task Manager and Code Registry

`.shproject` is the only canonical store for both: Markdown cards
(`tasks/cards/*.md`) and JSON records (`registry/records/*.json`). Everything
else — FTS-style lexical indexes, embeddings, scan/audit history, Executor
task claims — is a derived or runtime-only artifact and never blocks the
canonical read/write path if it's missing or stale.

An Audit Contract only ever runs structured, declared checks: `source:` (a
file contains a substring), `registry:` (an entry exists), and `validation:`
(a project-declared recipe from `.shproject/automation/validation-recipes.json`,
executed through the same deterministic-command primitive automation
pipelines already use). A card can never trigger a free-form command. A task
only auto-completes when it has at least one `source`/`registry` check and at
least one `validation` check, all resolved and passing; losing that evidence
on a later audit reopens it (`done` → `changes_requested`) instead of leaving
a stale status in place.

Code Registry's semantic search is additive, never load-bearing: lexical
search reads only the records already on disk and never depends on the
embedding engine being installed, downloading, or running. `internal/registry`
depends on embeddings only through the `Embedder` interface it declares
itself (`Embed(ctx, text) ([]float32, error)`) — it has no idea that
`internal/embedding` exists, let alone that it shells out to llama.cpp.

CLI, TUI, and Web Panel all call the same `internal/tasks`/`internal/registry`
services directly (through `*app.App` for the TUI/Web process, through a
lighter one-shot wiring in `cmd/sessionhub`'s CLI dispatch) — there is no
separate server process, no daemon, and no RPC layer between them.

## Remote boundary

Remote mode is not a public service. The host validates that the configured
listen address is a Tailscale CGNAT or Tailscale IPv6 address and binds only to
that address. The framed protocol carries snapshots, input, resize, control
leases, queue/pipeline operations, checkpoints, metrics, and logs. Executors
remain in the host process. Disconnect does not replay unacknowledged input.
