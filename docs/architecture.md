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
- `internal/gitstate`: optional Git workspace inspection.
- `internal/remote`: framed Tailscale host/client protocol.
- `internal/update`: GitHub release lookup and checksum-verified replacement
  preparation.
- `internal/ui`: Bubble Tea models and forms.

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

## Remote boundary

Remote mode is not a public service. The host validates that the configured
listen address is a Tailscale CGNAT or Tailscale IPv6 address and binds only to
that address. The framed protocol carries snapshots, input, resize, control
leases, queue/pipeline operations, checkpoints, metrics, and logs. Executors
remain in the host process. Disconnect does not replay unacknowledged input.
