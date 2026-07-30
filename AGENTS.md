# Guidelines and Rules for AI Agents (AGENTS.md)

This project, **Session Hub**, is maintained by **João Cunha** (`jgcastro09`). All AI coding agents working on this repository must strictly adhere to the following mandatory workflow rules:

---

## Mandatory Rules

### 1. Version Increment & Changelog (REQUIRED)
- **Every single change** (feature, bug fix, refactor, or configuration change) **MUST** be accompanied by a version bump and a `CHANGELOG.md` update.
- Files to update for version bumps:
  - [`VERSION`](file:///d:/BUILD_LAB/SESSIONHUB/VERSION)
  - [`cmd/sessionhub/main.go`](file:///d:/BUILD_LAB/SESSIONHUB/cmd/sessionhub/main.go) (`version` variable)
  - [`npm/package.json`](file:///d:/BUILD_LAB/SESSIONHUB/npm/package.json) (`version` field)

### 2. Timezone & Timestamp Format in Changelog
- Release entries in [`CHANGELOG.md`](file:///d:/BUILD_LAB/SESSIONHUB/CHANGELOG.md) **MUST** include the date and time in **Horário de Brasília / São Paulo, Brasil (UTC-3)**.
- Format: `YYYY-MM-DD HH:mm:ss -03:00`
- Example: `## [0.1.1] - 2026-07-30 10:45:00 -03:00`

### 3. Topbar UI Version Display
- The topbar in the user interface (defined in [`internal/ui/model.go`](file:///d:/BUILD_LAB/SESSIONHUB/internal/ui/model.go)) **MUST** always render the active software version (e.g., `SESSION HUB v0.1.1 ...`).

### 4. Mandatory Compilation & Test Verification
- **Never finish a task without compiling and testing.**
- After any code modification, run:
  1. `go build ./cmd/sessionhub`
  2. `go test ./...`
  3. `npm test` (inside `npm/` directory)

### 5. Software Identity & Aesthetics
- Preserving the visual design, state management, PTY integrity, and dark-theme aesthetic of Session Hub is critical.
- Maintain project ownership under João Cunha (`github.com/jgcastro09/sessionhub`).

### 6. Automated GitHub Releases & NPM Publishing
- The repository has `NPM_TOKEN` configured in GitHub Actions Secrets.
- Every release **MUST** include creating and pushing the corresponding git tag `vX.Y.Z` (e.g., `git tag v0.1.3` and `git push origin v0.1.3`).
- Pushing a `v*` tag triggers [.github/workflows/release.yml](file:///d:/BUILD_LAB/SESSIONHUB/.github/workflows/release.yml), which automatically builds multi-platform binaries via GoReleaser, creates a GitHub Release, and publishes the package to the official NPM registry.
