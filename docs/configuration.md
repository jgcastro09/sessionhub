# Configuration

Configuration is edited from the TUI and stored in SQLite. Session Hub ships
with no Executor records.

An Executor record contains a display name, executable, arguments, working
directory, optional shell invocation, environment entries, resume invocation,
prompt suffix, timeout, recognition rules, optional functional roles, model
label, tokenizer selection, and price-table reference.

Environment entries have a `secret` flag. Secret values are passed to the
child process but redacted as `***` from logs, reports, history exports, and
continuity packages.

Recognition rules may match process exit, exit code, literal text, regular
expression, native prompt return, stable-output duration, deterministic
command result, manual confirmation, or timeout. Silence alone is never
success. Ambiguous matches pause for confirmation and record all matching
rules.

The Hub escape is `f12`, the only key interpreted by Session Hub while the
embedded terminal owns focus — every other key (including keys the CLI itself
uses, like `esc` or `ctrl+p`) is encoded and passed straight to the PTY.

## Project configuration (`.shproject`)

Each Project's own configuration lives in `.shproject/` at its root, portable
and safe to commit:

```text
.shproject/
├── manifest.json
├── tasks/
│   ├── workflow.json          # inspectable status/transition graph
│   └── cards/TASK-0001.md     # one Markdown card per task
├── registry/
│   ├── config.json            # scan roots, extensions, exclusions
│   └── records/*.json         # scanned entries, grouped by category
└── automation/
    └── validation-recipes.json  # declared Audit Contract validation commands
```

`registry/config.json` and `tasks/workflow.json` are written with sensible
generic defaults the first time each service runs; nothing in either file
assumes a specific project's language or layout. `validation-recipes.json`
is the one exception: it is never auto-created, since a recipe like
`go-test` bakes in a language assumption a Python or Node project wouldn't
share. A recipe is a plain deterministic command:

```json
{
  "recipes": {
    "go-test": { "command": "go", "args": ["test", "./..."] }
  }
}
```

A card's Audit Contract can only ever reference a recipe already declared
here by name (`- validation: go-test`) — never a free-form command.
