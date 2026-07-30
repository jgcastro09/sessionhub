# Changelog

All notable changes to Session Hub are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-07-30

### Added

- Cross-platform terminal user interface built with Bubble Tea, Lip Gloss, and
  Bubbles.
- Generic, manual Executor configuration with no bundled providers.
- Real PTY sessions using native Unix PTYs and Windows ConPTY.
- Persistent sessions, terminal history, context, checkpoints, metrics,
  schedules, queues, pipelines, approvals, and recovery state in SQLite.
- Deterministic pipeline commands, workspace locks, budgets, retries, and
  idempotent execution.
- Tailscale-only remote host/client protocol with explicit terminal control.
- Git workspace summaries, release update checks, and the NPM binary installer.
- Cross-platform CI and reproducible release configuration.

[Unreleased]: https://github.com/nodestage/sessionhub/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/nodestage/sessionhub/releases/tag/v0.1.0
