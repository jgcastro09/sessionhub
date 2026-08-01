---
name: npm-release-verify
description: Verify a Session Hub GitHub Release has every platform binary uploaded and downloadable before running (or after having run) `npm publish`, so `npm install -g @jgcastro09/sessionhub` doesn't 404. Use before any npm publish for this project, and any time the user asks to "publish", "release", or reports an npm install failure.
---

# npm-release-verify

## Why this exists

`npm/install.mjs` does not vendor a binary. Every `npm install` of this
package — including everyone else's, forever, for that exact version —
downloads `sessionhub_<version>_<os>_<arch>.tar.gz` and `checksums.txt`
straight from:

```
https://github.com/jgcastro09/sessionhub/releases/download/v<version>/...
```

(see `npm/lib/platform.mjs`). Those assets are built and uploaded by the
`Release` GitHub Actions workflow (`.github/workflows/release.yml`,
goreleaser + a macOS voice-tools job), which only starts once the matching
`vX.Y.Z` git tag is pushed — a separate, asynchronous process from
`npm publish`. `npm publish` succeeds instantly regardless of whether that
workflow has finished. If you publish before it finishes (or if it fails),
every `npm install` for that version 404s until someone fixes it.

This already happened for v0.5.1 in this repo. This skill exists so it
never happens silently again. See AGENTS.md Rule 7 for the full mandated
publish ordering — this skill is step 3 of that ordering.

## When to run this

- **Before** running `npm publish`, after pushing the version tag (the
  normal, correct case — verify first, publish second).
- **After the fact**, if you're auditing whether a publish that already
  happened is actually installable, or the user reports an `npm install`
  404.

Never tell the user a publish/release is "done" without having run this
and gotten a clean pass.

## Steps

1. **Determine the version and expected asset list.**
   Read `npm/package.json`'s `version` field (or `VERSION` at repo root —
   they must match). Build matrix comes from `.goreleaser.yml`:
   `goos: [windows, darwin, linux]` × `goarch: [amd64, arm64]`, minus
   `windows/arm64` (explicitly ignored). So for version `X.Y.Z` the
   required asset set is exactly:
   - `sessionhub_X.Y.Z_darwin_amd64.tar.gz`
   - `sessionhub_X.Y.Z_darwin_arm64.tar.gz`
   - `sessionhub_X.Y.Z_linux_amd64.tar.gz`
   - `sessionhub_X.Y.Z_linux_arm64.tar.gz`
   - `sessionhub_X.Y.Z_windows_amd64.tar.gz`
   - `checksums.txt`

   (`sessionhub-voice-darwin.tar.gz` is also uploaded by the separate
   macOS voice-tools job but is not fetched by `install.mjs` — don't block
   on it, though its absence usually means that job failed and is worth a
   mention.)

2. **Confirm the tag was actually pushed and matches HEAD's version bump.**
   ```
   git tag --list vX.Y.Z
   git log -1 --format=%H vX.Y.Z
   ```
   If the tag doesn't exist yet, stop — publish ordering starts with
   tagging and pushing (AGENTS.md Rule 7, step 2), not with this skill.

3. **Find and wait for the Release workflow run for that tag.**
   ```
   gh run list --workflow=Release --limit 5
   ```
   Find the run whose branch/tag column is `vX.Y.Z`. If it's still
   `in_progress` or `queued`:
   ```
   gh run watch <run-id> --exit-status
   ```
   This blocks until the run finishes and exits non-zero if it failed —
   treat a failed run as a hard stop, do not proceed to publish. Diagnose
   with `gh run view <run-id> --log-failed` and fix (re-push a fixed tag,
   or `gh workflow run` / re-trigger as appropriate) before continuing.

4. **Verify every required asset is attached to the GitHub Release.**
   ```
   gh release view vX.Y.Z --json assets --jq '.assets[].name'
   ```
   Diff this against the required list from step 1. Any asset missing
   from this list is a hard stop — the workflow may have partially
   succeeded (e.g. goreleaser's job passed but the dependent
   macos-voice-tools job didn't run or failed after it).

5. **Verify every required asset actually downloads (not just listed).**
   A listed asset can still 404 or redirect wrong in edge cases, and this
   is the exact check that would have caught the v0.5.1 incident directly
   rather than inferring it from workflow status. For each required asset:
   ```
   curl -sL -o /dev/null -w '%{http_code} %{url_effective}\n' \
     "https://github.com/jgcastro09/sessionhub/releases/download/vX.Y.Z/<asset>"
   ```
   Every line must read `200`. Anything else (404, 302 to a login page,
   etc.) is a hard stop.

6. **Only after every check in steps 3–5 passes**, it is safe to run
   (or to have run) `npm publish --access public` from `npm/`. Report a
   clear pass/fail summary to the user — don't just say "looks good",
   name which assets were checked and that all returned 200.

## What NOT to do

- Don't run `npm publish` and then immediately declare success without
  doing steps 3–5 — `npm publish`'s own exit code says nothing about
  whether the GitHub Release side is ready.
- Don't skip step 5 and treat step 4's listing as sufficient — the goal is
  to reproduce exactly what `npm/install.mjs` will do, not just check that
  GitHub's UI shows a filename.
- Don't silently retry or "fix" a failed Release workflow by force-pushing
  or deleting/recreating the tag without telling the user first — that's a
  destructive, hard-to-reverse action on a public tag/release and needs
  the same confirmation any other destructive git operation would.
