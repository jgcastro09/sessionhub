# Automation

Queues and pipelines send prompts through the same PTY write lease used by a
human operator. A completion rule configured on the Executor must confirm an
item before the next eligible item is claimed. Without a reliable rule, the
item waits for manual confirmation.

Pipeline step types are Executor prompt, deterministic command, manual
approval, condition, parallel split, and consolidation. Dependencies form an
acyclic graph. A step claim, idempotency key, attempt count, and budget
reservation are committed before its external effect starts.

Schedules support one-time, daily, selected weekdays, and fixed intervals with
an IANA time zone. Missed-run policy is skip, run once, or ask. Reopening never
expands missed occurrences into an unbounded backlog.

Workspace locks are shared for read-only steps and exclusive for writing steps.
Conflicts pause affected work. Loops require at least one objective cap:
attempts, duration, total tokens, estimated cost, or cycles.
