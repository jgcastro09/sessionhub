# Session Hub

Session Hub is a terminal-native workspace for running multiple externally
installed AI CLIs in real pseudoterminals. It owns the global session,
continuity context, checkpoints, automation state, and metrics while each CLI
continues to own its native conversation and behavior.

Session Hub starts with **no Executors configured**. It does not install,
detect, authenticate, or embed provider-specific CLIs.

## Highlights

- Real PTYs: ConPTY on Windows and native PTYs on macOS/Linux.
- VT terminal emulation with ANSI styles, cursor state, alternate screen,
  Unicode, bracketed paste, mouse forwarding, and scrollback.
- Multiple live Executor instances per session without restarting on focus
  changes.
- Transactional SQLite persistence and crash recovery.
- Prompt queues, schedules, dependency pipelines, deterministic commands,
  approvals, retries, budgets, watchers, and workspace locking.
- Global context packages, checkpoints, Git summaries, and auditable metrics.
- Host/client operation restricted to selected Tailscale addresses.
- Reproducible binaries and an NPM installation channel.

## Install from source

Go 1.25 or newer is required.

```sh
go install github.com/nodestage/sessionhub/cmd/sessionhub@latest
sessionhub
```

Runtime data is stored in the platform user configuration directory unless
`SESSIONHUB_DATA_DIR` is set.

## First run

1. Start `sessionhub`.
2. Press `ctrl+g` to open Hub command mode.
3. Create a session and set its workspace.
4. Open Settings, add an Executor manually, and enter its executable,
   arguments, working directory, environment, completion rules, and metrics
   metadata.
5. Use **Test** to open the configuration in a real PTY. Save only after
   visually checking input, output, resize, and shutdown.
6. Start an instance and press `enter` to focus its native terminal.
7. Press `ctrl+]` (configurable) to return control to the Hub.

Authentication remains inside the configured external CLI.

## Documentation

- [Architecture](docs/architecture.md)
- [Configuration](docs/configuration.md)
- [Usage](docs/usage.md)
- [Automation](docs/automation.md)
- [Remote mode](docs/remote.md)
- [Installation and releases](docs/installation.md)
- [Validation matrix](docs/validation.md)
- [Dependencies](docs/dependencies.md)

## Security

Environment values marked secret are never shown in logs, reports, context
packages, or exports. Remote mode binds only to an explicitly selected
Tailscale address. Session Hub has no daemon: closing the TUI cancels
automation, closes remote access, stops child process trees, and persists
recovery records.

## License

MIT
