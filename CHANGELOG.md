# Changelog

All notable changes to Session Hub are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and versions follow
[Semantic Versioning](https://semver.org/).

## [Unreleased]

## [0.4.0] - 2026-07-31 13:55:49 -03:00

### Added

- Web Panel: a monitoring-only companion dashboard served straight from the SessionHub binary (`internal/webserver`), covering sessions, executors, aggregated token/cost metrics, logs, queue, schedules, and pipelines as read-only REST endpoints plus a `/api/events` Server-Sent Events stream for live updates.
- Terminal-styled, mobile-first web frontend (`web/`, React + Vite + TypeScript) built straight into `internal/webserver/dist` via `go:embed`, so the panel ships inside the single Go binary with no extra runtime dependency.
- Lightweight access control for the panel: a Tailscale-address trust check reusing `remote.IsTailscaleIP` (no code needed on the tailnet), and a short pairing code + HttpOnly cookie flow for LAN access, configurable per `Settings` in the TUI (`[w]` toggle, `[b]` cycle bind mode, `[g]` regenerate pairing code).
- New `ListSchedules`/`ListPipelines` store queries and `App.WebQueue`/`WebSchedules`/`WebPipelines` methods backing the panel's automation views.

Bumps version to 0.4.0.

## [0.3.51] - 2026-07-31 13:07:00 -03:00

### Removed

- Removed all emoji characters across status messages, modals, titles, and buttons in the terminal user interface.

Bumps version to 0.3.51.

## [0.3.50] - 2026-07-31 12:45:00 -03:00

### Removed

- Reverted logo and Dock icon changes per user request; restored pure terminal-native layout and behavior.

Bumps version to 0.3.50.

## [0.3.49] - 2026-07-31 12:35:00 -03:00

### Added

- Embedded multi-resolution `AppIcon.icns` asset and added automatic `SessionHub.app` bundle registration (`lsregister`) for macOS LaunchServices and Dock icon tile updates (`dockTile`).

Bumps version to 0.3.49.

## [0.3.48] - 2026-07-31 12:28:00 -03:00

### Added

- Embedded custom Session Hub logo assets (`internal/assets/logo.png`) and added dynamic macOS Dock icon activation (`setOSDockIcon`) when launching Session Hub via terminal.

Bumps version to 0.3.48.

## [0.3.47] - 2026-07-31 12:18:00 -03:00

### Added

- Added visual logo/icon indicators to topbar terminal tabs (`` Zsh, `🐚` Bash, `🐟` Fish, `⚡` PowerShell, `🖥️` CMD, `🐧` WSL, `🤖` Codex, `✳️` Claude, `🚀` OpenCode, `🌌` Antigravity) with support for custom `Icon` configuration.

Bumps version to 0.3.47.

## [0.3.46] - 2026-07-31 12:08:00 -03:00

### Added

- Automatically attached newly created system terminals and executors to the active session upon registration, making their tabs immediately visible and selectable (via click or `Alt+1`, `Alt+2`, etc.) in the topbar tab bar.

Bumps version to 0.3.46.

## [0.3.45] - 2026-07-31 12:00:00 -03:00

### Added

- Added OS-specific filtering (`CatalogForOS`) for System Terminals in the Add Terminal / CLI pick list. On macOS/Linux, Windows-only shells (`PowerShell`, `CMD`, `WSL`) are hidden; on Windows, macOS-only shells (`Zsh`) are hidden, presenting only OS-compatible shells.

Bumps version to 0.3.45.

## [0.3.44] - 2026-07-31 11:50:00 -03:00

### Added

- Added System Terminal aggregations (Zsh, Bash, Fish, PowerShell, CMD, WSL) to the Add Terminal/CLI catalog.
- Added `UseHostHome` option to `ExecutorConfig` so standard system shell tabs preserve the user's real host environment, dotfiles (`.zshrc`, `.bashrc`, `$PROFILE`), aliases, and custom prompts without HOME redirection.
- Supported running AI tools, scripts, and voice dictation directly inside system shell tabs.

Bumps version to 0.3.44.

## [0.3.43] - 2026-07-31 11:34:00 -03:00

### Added

- Enhanced CLI discovery with system `$PATH` and standard global directory scanning (`~/.local/bin`, `~/.npm-global/bin`, `/usr/local/bin`, `/opt/homebrew/bin`, `%APPDATA%\npm`, etc.). Adding a CLI that is already present on the machine registers instantly without re-executing `npm install`.
- Maintained profile and credential isolation for each registered executor (`executors/<slug>/config`), enabling users to create multiple accounts/profiles (e.g. `Claude Empresa A`, `Claude Empresa B`) that share a single system CLI binary while keeping login sessions separated.

Bumps version to 0.3.43.

## [0.3.42] - 2026-07-31 00:25:56 -03:00

### Fixed

- The Executor Command field (introduced in 0.3.40 to merge Command + Arguments into one free-text field) treated backslash as a shell escape character, so every Windows install path (e.g. `\executors\claude-code\node_modules\.bin\claude.CMD`) got mangled on parse and, to compensate, was wrapped in escaped quotes on display — showing as a garbled, seemingly fixed value with a stray trailing `"` and no visible room to add flags like `--dangerously-skip-permissions`. Backslash is now treated as an ordinary character (quoting is only applied for whitespace or embedded quote characters), so Windows paths display and round-trip untouched and the field is now obviously editable — append your flags after the path, same field.

Bumps version to 0.3.42.

## [0.3.41] - 2026-07-31 00:16:25 -03:00

### Changed

- The Executor edit form now opens with just the core fields (Display name, Command, Working directory), same as "new executor", instead of dumping all ten advanced fields (Environment, Resume command, Recognition rules, Roles, Shell, Timeout, Prompt suffix, Model label, Tokenizer, Price ID) at once. ctrl+a still reveals them, now prefilled from the existing config.
- Saving no longer silently drops advanced fields, install metadata (BinaryName/InstallDir), or CreatedAt when the operator only edits the core fields without expanding ctrl+a — they're carried through from the config being edited.

Bumps version to 0.3.41.

## [0.3.40] - 2026-07-30 23:14:31 -03:00

### Changed

- Simplified the Executor form: "Command" and "Arguments" are now a single free-text field (e.g. `codex --yolo`) instead of a command field plus a separate JSON-array field. Quotes allow an argument to contain spaces.
- "Resume args" was folded into "Resume command" the same way.
- "Environment", "Roles", and "Recognition rules" no longer require JSON syntax: Environment is `NAME=value` pairs separated by `;` (prefix a name with `*` to mark it secret), Roles is a comma-separated list, and Recognition rules is `name::kind::value::outcome` entries separated by `;;`.
- The "Add a CLI" catalog now pre-fills the Command field with each CLI's permission-bypass flag as an editable suggestion (`codex --yolo`, `claude --dangerously-skip-permissions`) so new executors open unlocked by default; remove the flag before saving if you don't want that.

Bumps version to 0.3.40.

### Fixed

- "Add a CLI" no longer writes the install manifest before the database save is confirmed. Previously, a lost or failed executor save (e.g. the shared SQLite connection was busy) still left behind a manifest claiming the CLI was installed, so every retry re-detected "already installed", repeated the same failed save, and silently never registered the executor.

## [0.3.38] - 2026-07-30 22:11:35 -03:00

### Fixed

- Windows self-update now stages the verified executable and replaces it only after SessionHub exits, instead of trying to rename the running `.exe`.
- Replaced all-motion mouse capture with cell-motion capture, preserving clickable UI controls without Windows Terminal pointer/escape-sequence rendering glitches.

## [0.3.37] - 2026-07-30 22:02:59 -03:00

### Fixed

- Closing a terminal that emitted a very large amount of output no longer waits indefinitely for every queued history write. Persisted history is retained while only the pending shutdown backlog is discarded, preventing CI release builds and SessionHub shutdown from hanging.

## [0.3.36] - 2026-07-30 21:46:56 -03:00

### Added

- Settings now presents a compact technical Remote Network control panel with prominent actions to enable/disable Remote Mode, toggle Tailscale separately, and restart/re-announce networking after Wi-Fi or VPN changes.
- Remote Mode enablement is persisted independently from Tailscale. Disabling it stops both the local host and discovery endpoint; the saved Tailscale preference is retained for the next enablement.

### Changed

- Settings now groups system paths, transport state, endpoint/config location, software update actions, and factory reset into concise operational sections.

## [0.3.35] - 2026-07-30 21:35:11 -03:00

### Added

- A SessionHub that is being remotely controlled now shows a locked, explicit Remote Control modal and mirrors the controller's selected section, session, and CLI tab in real time.
- The controlled computer can press `r` to revoke access. This immediately disconnects the controller and releases its remote terminal lease.

## [0.3.34] - 2026-07-30 21:24:52 -03:00

### Added

- Settings now includes a minimal Remote Network panel showing always-on LAN discovery, detected local IPs, Tailscale detection and its IPs, plus a persistent `t` toggle for Tailscale discovery.
- LAN and Tailscale Remote Mode are available simultaneously when enabled. Disabling Tailscale removes only Tailscale peer discovery; the local-network listener and discovery remain active.

## [0.3.33] - 2026-07-30 21:14:55 -03:00

### Fixed

- Remote terminal streaming no longer inherits the ten-second connection setup timeout. Once connected, a remote SessionHub remains available until the user returns to the local environment or either SessionHub closes.

## [0.3.32] - 2026-07-30 21:08:39 -03:00

### Fixed

- Remote Mode now obtains executor login/active status from the controlled computer instead of probing the controller's filesystem. Logged-in remote CLIs therefore remain shown as logged in, while login profiles and secret values stay exclusively on the remote host.

## [0.3.31] - 2026-07-30 21:00:55 -03:00

### Added

- Remote discovery now announces the SessionHub release version. Devices running a different version remain visible in the Remote list but are explicitly marked incompatible and cannot be selected for control.
- Remote control validates the exact SessionHub version in both the controller and host handshake, preventing incompatible peers from opening or operating a terminal even outside the normal UI.

## [0.3.30] - 2026-07-30 20:56:03 -03:00

### Fixed

- Closing SessionHub while it is being remotely controlled now closes the active Remote Mode socket before waiting for the host, preventing shutdown from blocking on a remote frame read.

## [0.3.29] - 2026-07-30 20:52:06 -03:00

### Added

- Remote Mode now starts automatically with SessionHub, discovers other open SessionHubs on the local network and Tailscale, and shows computer name, online status, network, and address in the Remote tab.
- Selecting an online device connects its SessionHub environment: remote sessions and executor tabs are loaded through the existing `StartOrReuse → PTY` flow, while keyboard input, paste, resize, and terminal snapshots stay on the selected remote terminal.
- Remote control is now explicitly one-to-one. A controlled SessionHub accepts one controller at a time, transfers only an idle local terminal lease when needed, and returns terminal control when the connection closes.
- Both sides visibly enter Remote Mode: the controller uses a green interface/banner, while the controlled computer uses amber with the controller name shown in the top bar.

## [0.3.28] - 2026-07-30 20:28:59 -03:00

### Fixed

- Automation now pastes a prompt and submits it with a separate physical Enter key after a short handoff delay. This makes Codex, Claude Code, and other full-screen CLIs execute the prompt instead of leaving it in their editor. Idle fallback completion now requires sustained terminal response activity, preventing an echoed but unsubmitted prompt from being marked completed.

## [0.3.27] - 2026-07-30 20:21:38 -03:00

### Fixed

- Automation History now captures the terminal emulator's rendered completion screen before saving its bounded output preview. This preserves the assistant's final text from full-screen CLIs such as OpenCode instead of saving only the last raw redraw/footer cells; raw PTY output remains a fallback.

## [0.3.26] - 2026-07-30 20:12:38 -03:00

### Added

- Automation now sends a preconfigured SessionHub-only instruction around the saved user task: it identifies the run as an automation, asks the executor to work autonomously, and requires an unambiguous final completion marker. The marker is recognized immediately by the scheduler but is removed from the saved History output; existing five-second idle completion remains the fallback for executors that do not emit it.

## [0.3.25] - 2026-07-30 20:07:41 -03:00

### Fixed

- Ruleless interactive executors such as OpenCode now complete an Automation step after producing output and remaining quiet for five seconds, instead of staying `Running` indefinitely after their answer. The History modal's terminal sanitizer now also removes true-color CSI and OSC control sequences left by full-screen TUIs.

## [0.3.24] - 2026-07-30 20:00:59 -03:00

### Fixed

- Automation no longer renders raw terminal snapshots in its list, preventing OpenCode ANSI background sequences from drawing a black panel over the screen. Live and final executor output is normalized to plain text and is available through the `History / Details` modal, together with the run metadata, errors, and bounded response preview.

## [0.3.23] - 2026-07-30 19:47:05 -03:00

### Fixed

- When Automation activates a previously offline CLI tab, it now waits five seconds for the CLI input layer to finish initializing, then still verifies the PTY's initial render is stable before sending the configured prompt. Existing already-open tabs are sent immediately.

## [0.3.22] - 2026-07-30 19:40:33 -03:00

### Fixed

- The topbar now marks a CLI tab online as soon as Automation activates its PTY, even before the UI has focused that tab. The indicator queries the existing in-memory executor registry, so it remains accurate without unnecessary database work during terminal redraws.

## [0.3.21] - 2026-07-30 19:31:46 -03:00

### Fixed

- Automation now releases the selected CLI back to the local operator immediately after its prompt is written. It continues observing the existing PTY output for completion without blocking clicks, typing, or normal workflow in that tab.

## [0.3.20] - 2026-07-30 19:25:08 -03:00

### Fixed

- Automation now waits for a newly activated CLI's own initial PTY render to settle before sending its prompt. This prevents OpenCode from receiving input during its startup screen, where the prompt was previously discarded. The Automation status now explicitly reports that it is opening the selected tab and waiting for readiness.

## [0.3.19] - 2026-07-30 19:18:37 -03:00

### Fixed

- Automation now activates an inactive executor tab through the same lazy PTY startup flow as clicking that tab in the topbar, then sends the prompt to that exact instance. The UI now discovers and attaches to an automation-activated tab instead of launching a second executor when the operator later selects it.

## [0.3.18] - 2026-07-30 19:12:12 -03:00

### Fixed

- Automation now targets only the selected Session's already-open CLI tab. It no longer starts a second hidden executor instance; if that tab is not open, the automation keeps retrying with a clear instruction to open the selected tab first.

## [0.3.17] - 2026-07-30 19:06:24 -03:00

### Fixed

- An automation now takes temporary control of the selected Session's already-open local CLI tab instead of waiting forever on the `local/operator` PTY lease. It sends the prompt through that same terminal, preserves remote and other automation ownership protections, and restores the local lease when the automation step ends.

## [0.3.16] - 2026-07-30 18:58:00 -03:00

### Added

- Running automations now show actionable live feedback in the Automation list: whether they are acquiring terminal control, waiting for executor completion, and a bounded live tail of the executor's existing PTY output after the prompt has been sent.

## [0.3.15] - 2026-07-30 18:39:44 -03:00

### Fixed

- Automation no longer fails an occurrence immediately when its executor PTY is temporarily owned by the local operator. It remains running, records the current cause in the list, and retries the current step every 10 seconds until it succeeds or the user cancels it. Missing sessions or executors remain explicit terminal failures.

## [0.3.14] - 2026-07-30 18:28:24 -03:00

### Changed

- Simplified the Automation editor to a selection-first flow: choose an existing Session, Executor, and `Once`/`Daily`/`Weekly` schedule directly in the interface, select weekly days when needed, and type only the prompt and time. Automation names are derived from the prompt; each automation now creates one straightforward executor prompt instead of exposing IDs, dates, enabled flags, or multi-step syntax.
- A one-time automation now automatically uses the next future occurrence of the selected time, so no date field is required.

## [0.3.13] - 2026-07-30 18:18:25 -03:00

### Added

- Added the first in-app Automation workspace. Automations persist in `~/.sessionhub/automations.json`, can target an existing Session, run once/daily/weekly, carry ordered executor-prompt steps, and expose New, Edit, Run Now, Cancel, Delete, and Last Run Details actions.
- Added an in-process scheduler that runs only while SessionHub is open. It never installs a system task or background daemon, never replays missed daily/weekly work, and records a past one-time occurrence as `Missed`.
- Added sequential automation execution through the existing Session → Executor → PTY path, including safe cancellation, duplicate-run protection, saved bounded output previews, and clear failures for deleted sessions or executors.

### Changed

- Executor completion now treats a normal process exit as completion evidence when no explicit exit recognition rule is configured; non-zero exits fail the active work. Interactive CLIs still use their configured recognition rules to signal completion.

## [0.3.12] - 2026-07-30 17:55:18 -03:00

### Added

- Added a visible microphone control to the top-right of the Session Hub interface. Click `🎙 MICROFONE` to start local live dictation; it changes to the red `■ PARAR` button while recording, and a second click stops it and transcribes the final words. It uses the same safe action path as F9 and stays outside the PTY viewport, so clicks never leak into the focused CLI.

## [0.3.11] - 2026-07-30 17:45:55 -03:00

### Changed

- Switched the default offline dictation model from Whisper `base` to the more accurate multilingual Whisper `small` model (`ggml-small.bin`, ~465MB). It remains SHA-256 verified and is downloaded only once into `~/.sessionhub/tools/whisper-models/`; existing `base` files are retained but no longer used by new dictation sessions.

## [0.3.10] - 2026-07-30 17:39:49 -03:00

### Added

- The first local Whisper setup now shows live, meaningful status in the UI: download stage, percentage, and transferred/total MB for both the platform tools and multilingual model, followed by verification and local server startup states instead of a generic waiting message.

### Changed

- The multilingual `ggml-base.bin` model is now stored once at `~/.sessionhub/tools/whisper-models/ggml-base.bin`, independent of macOS/Windows tool versions. It is reused across restarts and upgrades; existing verified model files are hard-linked into this shared location during the first upgrade, avoiding another ~141MB download.

## [0.3.9] - 2026-07-30 17:32:48 -03:00

### Added

- Voice dictation now appears progressively in the focused CLI while you speak instead of waiting for F9 to stop. Every two seconds, Session Hub safely snapshots the growing local WAV, asks the already-running local whisper.cpp server for the transcript so far, and pastes only the new words. F9 still performs one final pass for the tail of the recording, without duplicating already inserted text. This works on both macOS and Windows; no audio leaves the machine.

## [0.3.8] - 2026-07-30 16:54:51 -03:00

### Fixed

- Pinned the SHA-256 for v0.3.7's rebuilt `sessionhub-voice-darwin.tar.gz`, which contains the macOS recording-finalization fix. Verified the downloaded release asset against the published checksum, confirmed its universal recorder supports both `x86_64` and `arm64`, and recorded a non-empty WAV with that exact packaged binary. New macOS installs now fetch the corrected helper instead of the pre-fix v0.3.3 helper.

## [0.3.7] - 2026-07-30 16:49:00 -03:00

### Fixed

- macOS voice dictation could record successfully but fail when F9 stopped it with `exit status 3`. The native microphone helper blocked the main run loop with `dispatch_semaphore_wait` while it waited for AVFoundation's asynchronous WAV-finalization callback; when macOS scheduled that callback on the main run loop, it could never execute and the helper always hit its five-second timeout. It now keeps that run loop active both while recording and while waiting for the WAV to finish, so the callback can flush and confirm the file normally.

## [0.3.6] - 2026-07-30 16:30:00 -03:00

### Fixed

- v0.3.5's release build itself failed: the new `TestViewLatencyUnderHeavyOutput` tripped its own 50ms threshold on the shared GitHub Actions runner (`max=63.865247ms`) — that runner is slower/noisier than my dev machine, and the test's own chatty child already competes hard for the same CPU. Relaxed both new tests' thresholds to 300ms; they guard against the actual regression reported (multi-second-plus hangs), not a tight perf budget, so the extra headroom doesn't weaken what they catch.

## [0.3.5] - 2026-07-30 16:26:00 -03:00

### Fixed

- Keyboard input inside a focused CLI could feel like it "didn't register, then the previous key fired instead of the new one" while the CLI was actively producing output — i.e. most of the time you're actually using a streaming AI agent — on both Windows and macOS. Root-caused with real, reproducible measurements (not guesswork): every PTY output chunk went through `record()`, which persists to SQLite with `PRAGMA synchronous = FULL` — a full disk fsync per call. A chatty child (any LLM CLI streaming tokens or redrawing) could turn into hundreds of synchronous fsyncs per second, which alone was slow enough to degrade the whole app's responsiveness; on top of that, `SafeEmulator` guards both `SendKey` (input) and `Write` (output) with the same lock, so that many `Write()` calls per second also had more chances to contend with a keystroke. `readOutput` now coalesces PTY reads over an 8ms window before writing/recording/emitting them, cutting call frequency (and fsync count) by roughly the batching factor. Added `TestSendKeyLatencyUnderHeavyOutput` and `TestViewLatencyUnderHeavyOutput`, which reproduce heavy sustained CLI output and assert both stay under 50ms, so this can't silently regress.
- Separately, found and fixed real orphaned-process leaks on both platforms: `Session.Close` used to call plain `Process.Kill()`, which only terminates the single tracked PID, not any children it spawned (common for these CLIs, and for shell-wrapped executors). Over a long session with many tabs started/stopped, orphaned processes could accumulate and degrade overall machine performance — a plausible contributor to the same "impossible to work" symptom. Fixed with a `terminateProcessTree` helper (`taskkill /PID ... /T /F` on Windows, process-group `SIGKILL` on Unix — the same pattern already used in `internal/automation` for command-step subprocesses), verified by confirming no child processes are left behind after closing a session that had spawned some.

## [0.3.4] - 2026-07-30 15:41:00 -03:00

### Fixed

- Pinned the real sha256 for the fixed `sessionhub-voice-darwin.tar.gz` (v0.3.3's CI run). Verified independently: downloaded the asset, confirmed the checksum, confirmed every symlink `whisper-server` actually needs is present (cross-checked against `otool -L`'s own `@rpath` dependency list from the CI log — all 6 of `libwhisper.1.dylib`, `libggml.0.dylib`, `libggml-cpu.0.dylib`, `libggml-blas.0.dylib`, `libggml-metal.0.dylib`, `libggml-base.0.dylib` resolve). macOS voice dictation should now actually start.

## [0.3.3] - 2026-07-30 15:34:00 -03:00

### Fixed

- macOS voice dictation failed to start on real hardware: `whisper-server didn't come up: ... dyld: Library not loaded: @rpath/libwhisper.1.dylib`. Root cause confirmed by re-downloading v0.3.1/v0.3.2's own release asset: macOS dylibs ship as a fully-versioned real file (`libwhisper.1.9.1.dylib`) plus a shorter compat-version **symlink** (`libwhisper.1.dylib`) that the executables' load commands actually reference — the CI packaging step's `find -type f` silently dropped every such symlink, and `internal/voice/install_darwin.go`'s tar extraction only handled regular files too, so even a correctly-packaged archive would have been silently stripped of symlinks on the client side. Fixed both: the CI job now copies symlinks (`-type l`, `cp -P`) and prints `otool -L` for verification, and `extractTarGz` now recreates `tar.TypeSymlink` entries via `os.Symlink` instead of skipping them.
- `internal/voice/install_darwin.go`'s pinned tag moves to `v0.3.3` (checksum blank again until this tag's CI run publishes the fixed asset) since v0.3.1/v0.3.2's asset is the broken one.

## [0.3.2] - 2026-07-30 15:10:00 -03:00

### Fixed

- Pinned the real sha256 for `sessionhub-voice-darwin.tar.gz` into `internal/voice/install_darwin.go`, now that the `macos-voice-tools` CI job has actually run (fixed in v0.3.1) and published it against the `v0.3.1` tag. Verified independently: downloaded the real release asset, confirmed its checksum matches, and confirmed the archive contains everything expected (`whisper-server`, `whisper-cli`, all required `.dylib`s, and `sessionhub-voice-recorder`). macOS voice dictation should now actually work end-to-end rather than failing with the "checksum isn't pinned yet" message.

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
