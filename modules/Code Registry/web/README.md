# NodeStage Code Registry Explorer

This is the self-contained, read-only architecture and audit interface for the canonical Code Registry. It consumes only `../generated/site-data.json`; it never reads source files or modular registry JSON in the browser.

## Start

From the repository root:

```text
python "Code Registry/tools/code_registry.py" full
python "Code Registry/tools/code_registry.py" serve
```

Open `http://127.0.0.1:8765/web/`. The server runs in the foreground on localhost and stops with `Ctrl+C`. It disables browser caching and does not expose source files or modular registry data.

## Navigation model

- **Overview** summarizes audit health, coverage, product areas, modules, and relationship types.
- **Architecture** follows physical module ownership.
- **Product Areas** follows cross-cutting features such as Workspaces, Live, Mapping, Scene 3D, Timeline, Outputs, and Licensing.
- **Nodes** groups generators, effects, inputs, outputs, AI, particles, and node infrastructure.
- **Relationships** renders a bounded local graph with explicit depth and node limits.
- **Review Queue** exposes changed hashes and validation issues.
- **All Files** filters individual entries without loading a modular JSON manually.

Headers and implementations with the same directory and stem appear as one logical unit and remain individually inspectable. Graph fill color represents architectural role, while borders represent review state or criticality.

## Refresh contract

The browser is deliberately not realtime. After changing code:

1. Run `python "Code Registry/tools/code_registry.py" full`.
2. Select **Reload data** to run the complete local synchronization (scan, generated payloads, and audit), then refresh the Explorer state. It detects new, changed, moved, and deleted eligible source files.

This keeps the visual model consistent with the validated Registry and avoids watchers, permanent services, databases, or hidden mutation endpoints.

## Vendored graph renderer

`vendor/cytoscape.min.js` is Cytoscape.js 3.34.0, licensed under MIT and stored locally for deterministic offline use.

- Source package: `https://www.npmjs.com/package/cytoscape/v/3.34.0`
- Vendored SHA-256: `9c2a3bf2592e0b14a1f7bec07c03a54f16dedf32af9cd0af155c716aa6c87bc3`
- License: `vendor/LICENSE-cytoscape.txt`

There is no package manager, CDN dependency, React runtime, frontend compilation, API, authentication, or writable server endpoint.
