#!/usr/bin/env python3
"""Official command-line interface for the NodeStage Code Registry."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any, Iterable

from explorer_server import serve_explorer
from context_builder import file_context, locate_ui_surface, module_context, read_symbol, search_context
from git_audit import audit_git
from history_store import file_diff, recent_changes, synchronize_history
from semantic_index import synchronize_semantic
from registry_builder import (
    build_outputs,
    load_registry_entries,
    write_modular_registries,
)
from scanner import load_config, scan_repository
from validator import validate_registry


ROOT = Path(__file__).resolve().parents[2]
REGISTRY_ROOT = ROOT / "Code Registry"


def _parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Catalog, search, build, and audit maintained NodeStage code."
    )
    subparsers = parser.add_subparsers(dest="command", required=True)
    subparsers.add_parser("scan", help="Update automatic metadata and modular registries.")
    subparsers.add_parser("validate", help="Validate coverage, schema, hashes, and reviews.")
    subparsers.add_parser("audit", help="Print the compact audit summary.").add_argument(
        "--verbose", action="store_true", help="List every validation issue and scan event."
    )
    subparsers.add_parser("build", help="Generate consolidated and Explorer-ready payloads.")
    subparsers.add_parser("full", help="Run scan, build, validate, and compact audit.")
    serve = subparsers.add_parser("serve", help="Serve the read-only local Registry Explorer.")
    serve.add_argument("--port", type=int, default=8765, help="Localhost port (default: 8765).")
    serve.add_argument("--no-browser", action="store_true", help="Do not open the default browser.")
    subparsers.add_parser("mcp", help="Run the local read-only Code Registry MCP server on stdio.")
    subparsers.add_parser("git-audit", help="Read Git working tree, upstream, and Registry correlation.")
    history = subparsers.add_parser("history", help="List local change history captured by synchronization.")
    history.add_argument("--path", help="Registered path to filter.")
    history.add_argument("--limit", type=int, default=50)
    diff = subparsers.add_parser("diff", help="Show the latest local diff for one registered path.")
    diff.add_argument("path")
    diff.add_argument("--limit", type=int, default=800)
    context = subparsers.add_parser("context", help="Build a bounded context pack for an AI client.")
    context.add_argument("query", help="Registered path, module name, or search query.")
    context.add_argument("--module", action="store_true", help="Interpret query as a module match.")
    context.add_argument("--search", action="store_true", help="Interpret query as a local index search.")
    context.add_argument("--budget", type=int)
    context.add_argument("--no-source", action="store_true")
    surface = subparsers.add_parser("surface", help="Locate the owner and renderer of a visible UI surface.")
    surface.add_argument("query", help="Visible label or UI concept.")
    surface.add_argument("--limit", type=int, default=8)
    symbol = subparsers.add_parser("symbol", help="Read one bounded function or method body.")
    symbol.add_argument("path")
    symbol.add_argument("name")
    symbol.add_argument("--maximum-chars", type=int, default=16000)

    search = subparsers.add_parser("search", help="Search modular records without loading source files.")
    search.add_argument("query", nargs="?", default="", help="Space-separated terms; all must match (any order) across path and semantic metadata.")
    search.add_argument("--module", dest="module_name", help="Filter by module substring.")
    search.add_argument("--language", help="Filter by language substring.")
    search.add_argument("--status", choices=("active", "deprecated", "reviewed", "needs_review"))
    search.add_argument("--limit", type=int, help="Maximum number of compact results.")
    search.add_argument("--related", action="store_true", help="Also print confirmed related paths.")
    search.add_argument("--sort", choices=("path", "modified-oldest", "modified-newest"), default="path", help="Order matches by path or source modification time.")

    review = subparsers.add_parser("review", help="Confirm one inspected file's current semantic review.")
    review.add_argument("path", help="Repository-relative registered path.")
    review.add_argument("--description", help="Replace the one-sentence description when its role changed.")
    review.add_argument("--module", dest="module_name", help="Replace the semantic module when ownership changed.")
    review.add_argument("--responsibility", action="append", help="Replace responsibilities; repeat up to three times.")
    review.add_argument("--related", action="append", help="Replace confirmed related paths; repeat as needed.")
    return parser


def _load_entries(config: dict[str, Any]) -> list[dict[str, Any]]:
    entries, errors = load_registry_entries(ROOT, config)
    if errors:
        raise RuntimeError("; ".join(errors))
    return entries


def _search_text(entry: dict[str, Any]) -> str:
    symbols = entry.get("symbols", {})
    symbol_values = [item for values in symbols.values() for item in values]
    values: list[str] = [
        entry.get("path", ""),
        entry.get("filename", ""),
        entry.get("module", ""),
        entry.get("description", ""),
        entry.get("language", ""),
        *entry.get("responsibilities", []),
        *entry.get("relatedFiles", []),
        *entry.get("probableRelatedFiles", []),
        *symbol_values,
    ]
    return "\n".join(str(value) for value in values).casefold()


def _command_search(args: argparse.Namespace, config: dict[str, Any]) -> int:
    if not any((args.query, args.module_name, args.language, args.status)):
        print("Informe uma consulta ou um filtro de módulo, linguagem ou status.")
        return 2
    entries = _load_entries(config)
    query_terms = args.query.casefold().split()
    module_filter = (args.module_name or "").casefold()
    language_filter = (args.language or "").casefold()
    matches = []
    for entry in entries:
        if query_terms:
            entry_text = _search_text(entry)
            if not all(term in entry_text for term in query_terms):
                continue
        if module_filter and module_filter not in entry["module"].casefold():
            continue
        if language_filter and language_filter not in entry["language"].casefold():
            continue
        if args.status:
            if args.status in {"reviewed", "needs_review"} and entry["reviewStatus"] != args.status:
                continue
            if args.status in {"active", "deprecated"} and entry["status"] != args.status:
                continue
        matches.append(entry)
    limit = args.limit if args.limit is not None else int(config["searchDefaultLimit"])
    if limit < 1:
        print("--limit deve ser maior que zero.")
        return 2
    if not matches:
        print("Nenhum arquivo encontrado.")
        return 0
    if args.sort == "modified-oldest":
        matches.sort(key=lambda entry: str(entry.get("lastModifiedAt", "")))
    elif args.sort == "modified-newest":
        matches.sort(key=lambda entry: str(entry.get("lastModifiedAt", "")), reverse=True)
    else:
        matches.sort(key=lambda entry: str(entry["path"]).casefold())
    for index, entry in enumerate(matches[:limit]):
        if index:
            print()
        print(entry["path"])
        print(entry["description"])
        print(f"Última alteração: {entry.get('lastModifiedAt', 'desconhecida')} | Linhas: {entry.get('lineCount', 0)}")
        if args.related and entry["relatedFiles"]:
            print("Relacionados: " + ", ".join(entry["relatedFiles"]))
    if len(matches) > limit:
        print(f"\n{len(matches) - limit} resultado(s) adicional(is); refine a busca ou aumente --limit.")
    return 0


def _print_audit(audit: dict[str, Any], verbose: bool = False) -> None:
    counts = audit["counts"]
    if audit["status"] == "approved":
        print(f"Code Registry: {counts['found']} arquivos de código validados.")
        families = audit["families"]
        print(
            f"C/C++: {families.get('C/C++', 0)} | Web: {families.get('Web', 0)} | "
            f"Python: {families.get('Python', 0)} | Shaders: {families.get('Shaders', 0)} | "
            f"Build: {families.get('Build', 0)}"
        )
        print(
            f"Ausentes: {counts['missing']} | Órfãos: {counts['orphaned']} | "
            f"Pendentes: {counts['pending']}"
        )
        print("Status: aprovado.")
    else:
        print("Code Registry: validação reprovada.")
        print(
            f"{counts['found']} encontrados, {counts['registered']} registrados, "
            f"{counts['missing']} ausentes, {counts['pending']} pendentes."
        )
        print('Execute `python "Code Registry/tools/code_registry.py" audit --verbose` para detalhes.')
    if verbose:
        for category, values in audit.get("issues", {}).items():
            if not values:
                continue
            print(f"\n{category} ({len(values)}):")
            for value in values:
                print(f"- {value}")
        events = audit.get("events", {})
        for category in ("new", "changed", "moved", "removed"):
            values = events.get(category) or []
            if not values:
                continue
            print(f"\nscan:{category} ({len(values)}):")
            for value in values:
                if isinstance(value, dict):
                    print(f"- {value.get('from')} -> {value.get('to')}")
                else:
                    print(f"- {value}")


def _load_audit() -> dict[str, Any]:
    path = REGISTRY_ROOT / "generated" / "audit-summary.json"
    return json.loads(path.read_text(encoding="utf-8"))


def _command_scan(config: dict[str, Any]) -> int:
    entries, events = scan_repository(ROOT, config)
    synchronize_history(ROOT, config, entries, events)
    semantic = synchronize_semantic(ROOT, config, entries)
    build_outputs(ROOT, config, entries, events)
    print(
        f"Code Registry: scan concluído ({len(entries)} arquivos; "
        f"novos {len(events['new'])}, alterados {len(events['changed'])}, "
        f"movidos {len(events['moved'])}, removidos {len(events['removed'])}; embeddings {semantic.get('indexed', 0)})."
    )
    return 0


def _command_build(config: dict[str, Any]) -> int:
    entries = _load_entries(config)
    build_outputs(ROOT, config, entries)
    print(f"Code Registry: consolidados gerados para {len(entries)} arquivos.")
    return 0


def _command_validate(config: dict[str, Any]) -> int:
    audit = validate_registry(ROOT, config, persist_audit=True)
    if audit["status"] == "approved":
        print(f"Code Registry: validação aprovada para {audit['counts']['registered']} arquivos.")
        return 0
    _print_audit(audit)
    return 1


def _command_review(args: argparse.Namespace, config: dict[str, Any]) -> int:
    target = args.path.replace("\\", "/").removeprefix("./")
    entries = _load_entries(config)
    entry = next((item for item in entries if item["path"] == target), None)
    if entry is None:
        print(f"Arquivo não registrado: {target}. Execute scan antes da revisão.")
        return 1
    path = ROOT / target
    if not path.is_file():
        print(f"Arquivo inexistente: {target}. Execute scan para tratar remoção ou movimento.")
        return 1
    if args.responsibility and len(args.responsibility) > int(config["maximumResponsibilities"]):
        print(f"Use no máximo {config['maximumResponsibilities']} responsabilidades.")
        return 2
    if args.description is not None:
        entry["description"] = args.description.strip()
    if args.module_name is not None:
        entry["module"] = args.module_name.strip()
    if args.responsibility is not None:
        entry["responsibilities"] = [value.strip() for value in args.responsibility if value.strip()]
    if args.related is not None:
        entry["relatedFiles"] = sorted(
            {value.replace("\\", "/").removeprefix("./") for value in args.related}, key=str.casefold
        )
    entry["lastReviewedHash"] = entry["hash"]
    entry["reviewStatus"] = "reviewed"
    write_modular_registries(ROOT, config, entries)
    build_outputs(ROOT, config, entries)
    audit = validate_registry(ROOT, config, persist_audit=True)
    print(f"Code Registry: revisão confirmada para {target}.")
    if audit["status"] != "approved":
        print(f"Ainda existem {audit['counts']['errors']} inconsistência(s) no registro.")
        return 1
    return 0


def _command_full(config: dict[str, Any]) -> int:
    entries, events = scan_repository(ROOT, config)
    synchronize_history(ROOT, config, entries, events)
    synchronize_semantic(ROOT, config, entries)
    build_outputs(ROOT, config, entries, events)
    audit = validate_registry(ROOT, config, persist_audit=True)
    _print_audit(audit)
    return 0 if audit["status"] == "approved" else 1


def _registered_path(config: dict[str, Any], path: str) -> str:
    normalized = path.replace("\\", "/").removeprefix("./")
    if normalized not in {entry["path"] for entry in _load_entries(config)}:
        raise ValueError("O caminho solicitado não é um arquivo registrado.")
    return normalized


def _command_history(args: argparse.Namespace, config: dict[str, Any]) -> int:
    path = _registered_path(config, args.path) if args.path else None
    print(json.dumps({"changes": recent_changes(ROOT, config, path=path, limit=args.limit)}, ensure_ascii=False, indent=2))
    return 0


def _command_diff(args: argparse.Namespace, config: dict[str, Any]) -> int:
    print(json.dumps(file_diff(ROOT, config, _registered_path(config, args.path), limit=args.limit), ensure_ascii=False, indent=2))
    return 0


def _command_context(args: argparse.Namespace, config: dict[str, Any]) -> int:
    if args.module:
        payload = module_context(ROOT, config, args.query, budget=args.budget)
    elif args.search:
        payload = search_context(ROOT, config, args.query)
    else:
        payload = file_context(ROOT, config, _registered_path(config, args.query), budget=args.budget, include_source=not args.no_source)
    print(json.dumps(payload, ensure_ascii=False, indent=2))
    return 0


def _command_surface(args: argparse.Namespace, config: dict[str, Any]) -> int:
    print(json.dumps(locate_ui_surface(ROOT, config, args.query, limit=args.limit), ensure_ascii=False, indent=2))
    return 0


def _command_symbol(args: argparse.Namespace, config: dict[str, Any]) -> int:
    print(json.dumps(read_symbol(ROOT, config, _registered_path(config, args.path), args.name, maximum_chars=args.maximum_chars), ensure_ascii=False, indent=2))
    return 0


def _command_git_audit(config: dict[str, Any]) -> int:
    print(json.dumps(audit_git(ROOT, _load_entries(config)), ensure_ascii=False, indent=2))
    return 0


def main(argv: Iterable[str] | None = None) -> int:
    args = _parser().parse_args(list(argv) if argv is not None else None)
    try:
        config = load_config(ROOT)
        if args.command == "scan":
            return _command_scan(config)
        if args.command == "build":
            return _command_build(config)
        if args.command == "validate":
            return _command_validate(config)
        if args.command == "audit":
            try:
                audit = _load_audit()
            except (OSError, UnicodeDecodeError, json.JSONDecodeError):
                audit = validate_registry(ROOT, config, persist_audit=True)
            _print_audit(audit, args.verbose)
            return 0 if audit["status"] == "approved" else 1
        if args.command == "search":
            return _command_search(args, config)
        if args.command == "review":
            return _command_review(args, config)
        if args.command == "full":
            return _command_full(config)
        if args.command == "history":
            return _command_history(args, config)
        if args.command == "diff":
            return _command_diff(args, config)
        if args.command == "context":
            return _command_context(args, config)
        if args.command == "surface":
            return _command_surface(args, config)
        if args.command == "symbol":
            return _command_symbol(args, config)
        if args.command == "mcp":
            from mcp_server import serve
            return serve()
        if args.command == "git-audit":
            return _command_git_audit(config)
        if args.command == "serve":
            return serve_explorer(REGISTRY_ROOT, args.port, open_browser=not args.no_browser)
    except (OSError, UnicodeError, json.JSONDecodeError, RuntimeError, ValueError) as exc:
        print(f"Code Registry: falha operacional: {exc}")
        return 1
    return 2


if __name__ == "__main__":
    sys.exit(main())
