# Usage

Session Hub opens on the current session and terminal. The sidebar lists
sessions and live instances; the top bar shows session, workspace, active
Executor, Git branch, and process state. The status bar shows token totals,
estimated API-equivalent cost, duration, control owner, and key hints.

Core keys:

| Key | Action |
| --- | --- |
| `f12` | Leave terminal focus (the only key that does — everything else, including `esc`/`ctrl+g`/`ctrl+p`/`ctrl+b`/`ctrl+q`, is passed straight through to the focused CLI) |
| `enter` | Focus or activate the selected item |
| `tab` / `shift+tab` | Change Hub section |
| `ctrl+g` | Open Hub command mode (Hub mode only) |
| `ctrl+p` | Open command palette (Hub mode only) |
| `ctrl+b` | Toggle sidebar and resize PTY (Hub mode only) |
| `ctrl+f` | Toggle terminal focus layout and resize PTY (Hub mode only) |
| `ctrl+c` | Quit (Hub mode only); while terminal-focused it is passed to the CLI |
| `q` | Quit from Hub mode |

Command mode exposes Sessions, Executors, Queues, Pipelines, Automations,
Metrics, Logs, Remote, and Settings. Destructive actions distinguish stopping
a process, removing its session association, and deleting persisted history.
They require separate confirmation.

## Factory Reset

The Settings tab has a Factory Reset that wipes the entire data directory —
every session, executor, login, log, and downloaded file — back to a clean
first-install state. It cannot be undone, so it's gated behind 3 steps:

1. `ctrl+r` in the Settings tab.
2. Confirm the y/n warning.
3. Type the exact phrase `DELETE EVERYTHING` and press `ctrl+s`.

Session Hub then quits; the next launch recreates the data directory from
scratch, exactly like a fresh install.
