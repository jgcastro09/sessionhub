# Validation matrix

Validation evidence is updated for each release.

Evidence for `v0.1.0`, recorded 2026-07-30:

| Target | Build | Automated tests | Manual PTY |
| --- | --- | --- | --- |
| Windows amd64 | Passed locally and in CI | 24 Go tests under the race detector, vet, ConPTY interaction, and nested TUI startup passed in CI; local suite also passed | Not executed in a human-operated terminal |
| macOS amd64 | Cross-build passed locally and in CI | Hosted macOS PTY/race/vet suite passed; runner architecture was not explicitly recorded | Not executed |
| macOS arm64 | Cross-build passed locally and in CI | Hosted macOS PTY/race/vet suite passed; runner architecture was not explicitly recorded | Not executed |
| Linux amd64 | Cross-build passed locally and in CI | Ubuntu PTY/race/vet suite passed | Not executed |
| Linux arm64 | Cross-build passed locally and in CI | Runtime tests were not executed on an ARM64 runner | Not executed |

GitHub Actions run `30515379848` passed every test, cross-build, and NPM job.
The local Windows ConPTY integration covered process startup/shutdown, input
and output, ANSI screen rendering, Unicode (`Olá 世界`), resize from 80×24 to
100×30, and a nested Session Hub TUI launch/quit. GitHub Actions results are
separate release evidence and are not claimed until the workflows complete.

Local `go test -race` was not executed because the available Windows Go
toolchain has `CGO_ENABLED=0`; the hosted Windows, macOS, and Ubuntu jobs ran
the race detector successfully.

Cross-compilation proves build compatibility only. It is not recorded as a
manual PTY validation. Manual records include OS version, terminal, dimensions,
executable used, ANSI/alternate-screen/Unicode/resize/interrupt checks, and
shutdown result.
