# Installation and releases

## GitHub release

Release archives are named:

```text
sessionhub_VERSION_GOOS_GOARCH.tar.gz
```

The release workflow builds Windows amd64, Darwin amd64/arm64, and Linux
amd64/arm64 binaries with stripped source paths, commit/version metadata, and
a single `checksums.txt`.

## NPM

```sh
npm install -g sessionhub
sessionhub
```

The NPM install script maps the local platform and architecture to a GitHub
Release asset, downloads the matching checksum list and archive, verifies
SHA-256 before extraction, and installs only the Go executable. Node.js is not
used when the command runs.

## Updating

The TUI can check the official GitHub Releases API. An update is downloaded to
a staging path, matched against the release checksum, and presented for
confirmation. Replacement occurs only during a controlled restart; the
database and configuration directory are not inside the binary directory.
