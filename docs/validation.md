# Validation matrix

Validation evidence is updated for each release.

Evidence for `v0.1.0`, recorded 2026-07-30:

| Target | Build | Automated tests | Manual PTY |
| --- | --- | --- | --- |
| Windows amd64 | Passed locally with Go 1.26.5 | 24 Go tests, 3 NPM tests, vet, ConPTY interaction and nested TUI startup at 100×30 passed locally | Not executed in a human-operated terminal |
| macOS amd64 | Cross-build passed locally | Not executed locally; CI configured | Not executed |
| macOS arm64 | Cross-build passed locally | Not executed locally; CI configured | Not executed |
| Linux amd64 | Cross-build passed locally | Not executed locally; CI configured | Not executed |
| Linux arm64 | Cross-build passed locally | Not executed locally; CI configured | Not executed |

The local Windows ConPTY integration covered process startup/shutdown, input
and output, ANSI screen rendering, Unicode (`Olá 世界`), resize from 80×24 to
100×30, and a nested Session Hub TUI launch/quit. GitHub Actions results are
separate release evidence and are not claimed until the workflows complete.

Local `go test -race` was not executed because the available Windows Go
toolchain has `CGO_ENABLED=0`; CI runs the race detector on hosted toolchains.

Cross-compilation proves build compatibility only. It is not recorded as a
manual PTY validation. Manual records include OS version, terminal, dimensions,
executable used, ANSI/alternate-screen/Unicode/resize/interrupt checks, and
shutdown result.
