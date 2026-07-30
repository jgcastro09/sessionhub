# Changelog

All notable changes to Session Hub are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.3.1] - 2026-07-30 15:03:00 -03:00

### Fixed

- The new `macos-voice-tools` CI job (added in v0.3.0) was never actually running: it `needs: goreleaser`, and that job's own "Publish to NPM" step always fails in CI by this repo's convention (npm publish is done manually — AGENTS.md rule 6), which GitHub Actions treats as the whole job failing and skips anything depending on it. Added `if: ${{ !cancelled() }}` so it runs regardless of that unrelated, expected failure. `internal/voice/install_darwin.go`'s pinned tag moves to `v0.3.1` to match wherever the asset actually lands.

## [0.3.0] - 2026-07-30 14:57:00 -03:00

### Added

- Voice dictation (`f9`) now works on **macOS**, not just Windows. Neither a pure-Go CoreAudio capture library nor an official whisper.cpp macOS binary exists, so Session Hub's own release pipeline now builds both from source for macOS (a new `macos-voice-tools` CI job: whisper.cpp via cmake, plus a small native recorder helper at `native/macos/recorder.m` using AVFoundation) and ships them as a release asset, the same self-contained "downloads what it needs" philosophy as Windows. The main `sessionhub` binary itself is untouched — still `CGO_ENABLED=0` on every platform; the native helper is a separate process invoked via `os/exec`, exactly like `whisper-server` already is on Windows.
- `internal/voice/install.go` split into shared logic plus `install_windows.go`/`install_darwin.go`/`install_other.go` per platform.

### Known limitation

- The macOS asset's checksum needs to be pinned into `internal/voice/install_darwin.go` after the `macos-voice-tools` CI job runs for this tag for the first time — dictation on macOS will fail with a clear message until that one-time step is done. Also: this is the one piece of this feature not personally verified end-to-end before release (no Mac available) — real-hardware testing is expected as the next step.

## [0.2.0] - 2026-07-30 14:34:00 -03:00

### Added

- Voice dictation: press `f9` while a CLI tab is focused to record from the microphone, press it again to transcribe locally and paste the text into that tab. Fully offline — the first use downloads a self-contained [whisper.cpp](https://github.com/ggml-org/whisper.cpp) server plus a multilingual model (~150MB, one-time, sha256-verified) into the data directory, then keeps the transcription server running in the background so later dictations don't reload the model. Nothing is sent to any cloud API.
- Windows-only for now (WASAPI capture via the pure-Go `github.com/moutend/go-wca`/`github.com/go-ole/go-ole`, no cgo — the existing `CGO_ENABLED=0` cross-build for windows/darwin/linux stays intact; other platforms show a clear "not supported yet" message instead of failing silently).

## [0.1.22] - 2026-07-30 14:01:00 -03:00

### Changed

- Automatic update checks now run every 5 minutes (was every 30 seconds), staying comfortably under GitHub's unauthenticated API rate limit (60 requests/hour) for long-running sessions.

## [0.1.21] - 2026-07-30 13:55:00 -03:00

### Changed

- Automatic update checks now run every 30 seconds instead of every 30 minutes. Note: GitHub's unauthenticated API rate limit is 60 requests/hour, so a session left open for more than ~30 minutes will start hitting "Update check failed" once that budget is spent for the hour.

## [0.1.20] - 2026-07-30 13:47:00 -03:00

### Fixed

- Trackpad/mouse-wheel scrolling now works inside full-screen CLIs (opencode, vim, htop, and any other alt-screen app). Previously every wheel up/down was unconditionally intercepted to pan the Hub's own scrollback, so the wheel event never reached the focused app at all. Now the Hub only does that for a plain shell prompt (no alt-screen); once the CLI enters its full-screen UI, wheel events are forwarded to it like any other mouse event, so its own scrolling works.

## [0.1.19] - 2026-07-30 13:38:00 -03:00

### Added

- Factory Reset in the Settings tab: wipes every session, executor, login, log, and downloaded file, resetting the data directory back to a clean first-install state. Gated behind 3 steps so it can't be triggered by accident — `ctrl+r` to begin, a y/n confirm, then typing the exact phrase `"DELETE EVERYTHING"` — and the app quits after wiping so the next launch starts completely fresh.

## [0.1.18] - 2026-07-30 13:16:00 -03:00

### Fixed

- Terminal focus now locks down to a single exit shortcut, `f12`. Previously `esc`, `ctrl+g`, `ctrl+p`, `ctrl+b`, and `ctrl+q` also silently dropped focus back to the Hub, which collided with the focused CLI's own bindings (e.g. opencode's own `esc`/`ctrl+p`) — the Hub would grab the keystroke, focus would flip without warning, and the next `enter`/arrow key would land on the Hub's session list instead of the CLI, producing duplicated tab activations and seemingly "stuck" input. All keys except `f12` now pass straight through to the focused CLI, and `f12` was chosen so the exit shortcut never requires Shift regardless of keyboard layout.

### Changed

- `docs/usage.md` and `docs/configuration.md` updated to document the single `f12` focus-exit shortcut.

### Changed

- New sessions now require a workspace directory (no longer silently falls back to the current working directory), and leaving the session name blank auto-fills it with the workspace folder's base name.

## [0.1.16] - 2026-07-30 12:33:00 -03:00

### Changed

- Enhanced visual presentation of `keyStyle` keyboard shortcut legends and path formatting in `Executors` and `Sessions` tabs.

## [0.1.15] - 2026-07-30 12:31:30 -03:00

### Changed

- Redesigned the `Executors` and `Sessions` TUI views: eliminated cramped layout and wall-of-text help paragraphs, introduced smart path shortening (`shortenPath`), green status badges (`● activated`), and structured keyboard shortcut toolbars.

## [0.1.14] - 2026-07-30 12:25:30 -03:00

### Changed

- Consolidated redundant sidebar lists: removed the duplicate `Live terminals` section and unified live running terminal indicators and click-to-focus behavior into the single `Executors` list in the sidebar.

## [0.1.13] - 2026-07-30 12:18:45 -03:00

### Fixed

- Fixed `renderSidebar` container height calculation: previously hardcoded to `m.height - 2`, which when an active session tab bar was rendered (`activeSession >= 0`), caused `renderCenter` height to overflow by 1 line and push `renderBottom()` status bar off the bottom of the screen. `renderSidebar` now uses the dynamic center height from `m.terminalSize()`, ensuring the bottom footer is always 100% visible on screen.

## [0.1.12] - 2026-07-30 12:14:30 -03:00

### Fixed

- Fixed session working directory handling: when an Executor CLI tab is launched inside a session, `Service.Start` now automatically inherits the Session's `Workspace` as the PTY working directory (`cmd.Dir`).
- Fixed path input parsing in session creation and edit forms to strip surrounding double and single quotes (`"'`).

## [0.1.11] - 2026-07-30 12:10:30 -03:00

### Fixed

- Fixed TUI modal rendering where form and confirmation popups wiped out top header and bottom footer status bars. Modals are now rendered as overlays via `overlayModal`, keeping `renderTop()` and `renderBottom()` 100% persistent across all view states.
- Fixed update flow status tracking (`isCheckingUpdate`, `isUpdating`, update available, up to date) to ensure errors and status updates are consistently captured and displayed in the bottom footer bar.

### Added

- Added an interactive **Software Update** section in the *Settings* tab displaying active version, check status, release URL, and keyboard trigger instructions (`u` / `Enter`).

## [0.1.10] - 2026-07-30 12:01:00 -03:00

### Fixed

- Fixed a self-deadlock in the Executor service event loop that froze every
  terminal start after the first tab produced output: `handleEvent` held the
  service mutex while calling `handleOutput`/`finishWork`, which re-acquire the
  same (non-reentrant) mutex. In practice this made Alt+2 (or clicking a second
  tab) silently spawn orphaned CLI processes without ever switching focus, so a
  session could never run two CLI tabs at the same time. The lock is now
  released before those calls, and an end-to-end regression test covers
  starting, switching, and reattaching two tabs via Alt+1/Alt+2.

## [0.1.9] - 2026-07-30 11:31:00 -03:00

### Changed

- Updated `AGENTS.md` rule #6 mandating direct terminal publishing inside `npm/` via `npm publish --access public`.

### Added

- In-TUI real-time background update checker and interactive self-updater system with SHA-256 verification and Windows `.old` binary replacement support.

### Added

- Successfully published official package `@jgcastro09/sessionhub` to NPM registry.

### Changed

- Updated NPM package name to `@jgcastro09/sessionhub` for user-scoped NPM publishing.

### Changed

- Removed `AGENTS.md` from git tracking and added it to `.gitignore` to keep AI developer guidelines strictly local.

### Fixed

- Updated `.gitignore` to allow tracking `npm/bin/sessionhub.js` for NPM releases.

### Added

- Added rule #6 to `AGENTS.md` specifying automated GitHub Release and NPM publishing workflow when pushing version tags (`v*`).

### Added

- Automated `npm publish` step to GitHub Release workflow triggered on `v*` tag pushes.

### Changed

- Updated project repository and ownership to João Cunha (`github.com/jgcastro09/sessionhub`).
- Added application version display to the UI topbar header.
- Added project AGENTS.md with mandatory rules for versioning, changelog, UTC-3 timestamping, compilation, and topbar version display.
- Updated NPM package configuration and license attribution to João Cunha.

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

[Unreleased]: https://github.com/jgcastro09/sessionhub/compare/v0.1.9...HEAD
[0.1.9]: https://github.com/jgcastro09/sessionhub/compare/v0.1.8...v0.1.9
[0.1.8]: https://github.com/jgcastro09/sessionhub/compare/v0.1.7...v0.1.8
[0.1.7]: https://github.com/jgcastro09/sessionhub/compare/v0.1.6...v0.1.7
[0.1.6]: https://github.com/jgcastro09/sessionhub/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/jgcastro09/sessionhub/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/jgcastro09/sessionhub/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/jgcastro09/sessionhub/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/jgcastro09/sessionhub/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/jgcastro09/sessionhub/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/jgcastro09/sessionhub/releases/tag/v0.1.0
