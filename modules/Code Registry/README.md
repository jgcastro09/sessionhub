# NodeStage Code Registry

The Code Registry is the canonical, machine-readable index of source and build code maintained by NodeStage. It records one entry per file, keeps automatic facts separate from semantic review, and provides compact search and audit commands for agents and developers.

The registry does not store source contents, use a database, or replace reading the real implementation. Its optional localhost server is foreground-only and read-only; use the Registry to locate the smallest relevant set of files, then inspect those files directly.

## Commands

Run commands from the repository root:

```text
python "Code Registry/tools/code_registry.py" search "MIDI"
python "Code Registry/tools/code_registry.py" search --module "Mapping"
python "Code Registry/tools/code_registry.py" search --language "GLSL"
python "Code Registry/tools/code_registry.py" scan
python "Code Registry/tools/code_registry.py" build
python "Code Registry/tools/code_registry.py" validate
python "Code Registry/tools/code_registry.py" audit
python "Code Registry/tools/code_registry.py" audit --verbose
python "Code Registry/tools/code_registry.py" full
python "Code Registry/tools/code_registry.py" git-audit
python "Code Registry/tools/code_registry.py" serve
```

`full` is the required final command after changing registered code. It scans, rebuilds generated data, validates every entry and prints a compact audit.

After inspecting a changed file and confirming that its semantic metadata is still correct, acknowledge the current hash with:

```text
python "Code Registry/tools/code_registry.py" review "app/src/example/File.cpp"
```

Pass `--description`, `--module`, repeated `--responsibility`, or repeated `--related` only when the file's role actually changed.

## Data ownership

- `registry/*.json` contains the reviewed modular records and is the semantic source of truth.
- `generated/code-registry.json` is the consolidated deterministic index.
- `generated/audit-summary.json` contains compact validation counts and issue details.
- `generated/site-data.json` is the ready-to-consume Explorer payload, including architecture, product areas, logical units, graph relations, filters, counts, and audit state.
- `config.json` defines eligible code, explicit build files, categories, and justified exclusions.
- `explorer.json` defines versioned product-area membership, architecture-role inference, and graph limits without duplicating tags into every source record.
- `registry.schema.json` defines the required entry contract.

The scanner owns hashes, sizes, line counts, syntax summaries, probable relations, and scan timestamps. It preserves descriptions, responsibilities, confirmed related files, modules, criticality, and reviewed hashes.

Every maintained file must resolve to at least one Product Area. Validation fails when a new module or path is not covered by `explorer.json`, keeping the visual architecture complete as the repository grows.

## Local Architecture Explorer

### One-click launchers

- Windows: double-click `Code Registry/tools/Start-Code-Registry.bat`.
- macOS: make `Code Registry/tools/Start-Code-Registry.command` executable once with `chmod +x "Code Registry/tools/Start-Code-Registry.command"`, then double-click it in Finder.

Both launchers generate the current Registry data before starting the local Explorer. They still open the Explorer when the audit finds files that need semantic review, so the Review Queue and Uncategorized tabs remain available.

The read-only Explorer under `web/` provides Overview, Architecture, Product Areas, Nodes, Relationships, Code Reader, Global Search, Git Audit, Review Queue, and All Files views. Overview reports the total maintained code lines; All Files and Review Queue expose each source file's filesystem modification timestamp and can order files by oldest or newest change. Code Reader serves only files already registered in the inventory, from the localhost Explorer process, with a 2 MB file limit. Git Audit correlates Git working-tree state, commits, upstream divergence, conflicts, and registered-file reviews; its remote check only runs `git fetch --prune origin` and never pulls, merges, commits, or pushes.

Generate current data and start the localhost-only foreground server:

```text
python "Code Registry/tools/code_registry.py" full
python "Code Registry/tools/code_registry.py" serve
```

The default address is `http://127.0.0.1:8765/web/`. Use `--port` to select another port or `--no-browser` to avoid opening the default browser. The server exposes only the Explorer assets, `site-data.json`, `audit-summary.json`, and a health response; modular registries and source files are not served.

After source changes, run `full` and select **Reload data** in the page. Responses disable caching, so no watcher, WebSocket, background service, database, API, Node.js installation, or frontend build is required.
