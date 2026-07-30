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

The default Hub escape is `ctrl+]`. It is configurable and is only interpreted
while the embedded terminal owns focus. Other keys are encoded and passed to
the PTY.
