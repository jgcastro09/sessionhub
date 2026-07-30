# Usage

Session Hub opens on the current session and terminal. The sidebar lists
sessions and live instances; the top bar shows session, workspace, active
Executor, Git branch, and process state. The status bar shows token totals,
estimated API-equivalent cost, duration, control owner, and key hints.

Core keys:

| Key | Action |
| --- | --- |
| `ctrl+g` | Open Hub command mode |
| `ctrl+]` | Leave terminal focus |
| `enter` | Focus or activate the selected item |
| `tab` / `shift+tab` | Change Hub section |
| `ctrl+p` | Open command palette |
| `ctrl+b` | Toggle sidebar and resize PTY |
| `ctrl+f` | Toggle terminal focus layout and resize PTY |
| `ctrl+c` | Cancel the selected Hub operation; while terminal-focused it is passed to the CLI |
| `q` | Quit from Hub mode |

Command mode exposes Sessions, Executors, Queues, Pipelines, Automations,
Metrics, Logs, Remote, and Settings. Destructive actions distinguish stopping
a process, removing its session association, and deleting persisted history.
They require separate confirmation.
