#!/usr/bin/env python3
"""Strict coverage, schema, hash, review, and generated-data validation."""

from __future__ import annotations

import json
import re
from datetime import datetime
from pathlib import Path
from typing import Any

from explorer_builder import build_explorer_payload, load_explorer_config, validate_explorer_config
from registry_builder import base_audit, load_registry_entries, write_validation_results
from scanner import category_for, discover_files, module_for
from source_analyzer import analyze_file


ARRAY_FIELDS = {
    "responsibilities", "relatedFiles", "probableRelatedFiles", "previousPaths",
    "dependencies", "includes", "imports", "exports",
}
STRING_FIELDS = {
    "path", "filename", "extension", "language", "kind", "category", "module",
    "description", "criticality", "hash", "lastReviewedHash", "reviewStatus", "status",
    "lastScannedAt", "lastModifiedAt",
}
INTEGER_FIELDS = {"lineCount", "sizeBytes"}
HASH_RE = re.compile(r"^[0-9a-f]{64}$")
GENERIC_DESCRIPTIONS = (
    "arquivo responsável por funcionalidades",
    "arquivo de código",
    "implementa funcionalidades do sistema",
)


def _add(issues: dict[str, list[str]], category: str, message: str) -> None:
    if message not in issues[category]:
        issues[category].append(message)


def _validate_schema_entry(
    entry: dict[str, Any],
    required: set[str],
    allowed: set[str],
    config: dict[str, Any],
    issues: dict[str, list[str]],
) -> None:
    path = str(entry.get("path", "<sem caminho>"))
    missing = sorted(required - set(entry))
    extra = sorted(set(entry) - allowed)
    if missing:
        _add(issues, "schema", f"{path}: campos obrigatórios ausentes: {', '.join(missing)}")
    if extra:
        _add(issues, "schema", f"{path}: campos fora do schema: {', '.join(extra)}")
    for field in STRING_FIELDS & set(entry):
        if not isinstance(entry[field], str):
            _add(issues, "schema", f"{path}: {field} deve ser string")
    for field in INTEGER_FIELDS & set(entry):
        if not isinstance(entry[field], int) or isinstance(entry[field], bool) or entry[field] < 0:
            _add(issues, "schema", f"{path}: {field} deve ser inteiro não negativo")
    for field in ARRAY_FIELDS & set(entry):
        value = entry[field]
        if not isinstance(value, list) or any(not isinstance(item, str) or not item.strip() for item in value):
            _add(issues, "schema", f"{path}: {field} deve ser lista de strings não vazias")
        elif len(value) != len(set(value)):
            _add(issues, "schema", f"{path}: {field} contém valores duplicados")
    symbols = entry.get("symbols")
    if not isinstance(symbols, dict) or any(
        not isinstance(key, str)
        or not isinstance(value, list)
        or any(not isinstance(item, str) for item in value)
        for key, value in (symbols.items() if isinstance(symbols, dict) else [])
    ):
        _add(issues, "schema", f"{path}: symbols deve mapear nomes para listas de strings")

    if isinstance(entry.get("path"), str):
        if "\\" in entry["path"] or entry["path"].startswith("/") or ":" in entry["path"].split("/")[0]:
            _add(issues, "schema", f"{path}: caminho deve ser relativo e usar barras normais")
        if entry.get("filename") != Path(entry["path"]).name:
            _add(issues, "schema", f"{path}: filename não corresponde ao caminho")
    if not isinstance(entry.get("hash"), str) or not HASH_RE.fullmatch(entry.get("hash", "")):
        _add(issues, "schema", f"{path}: hash SHA-256 inválido")
    reviewed_hash = entry.get("lastReviewedHash", "")
    if not isinstance(reviewed_hash, str) or (reviewed_hash and not HASH_RE.fullmatch(reviewed_hash)):
        _add(issues, "schema", f"{path}: lastReviewedHash inválido")
    if entry.get("reviewStatus") not in {"reviewed", "needs_review"}:
        _add(issues, "schema", f"{path}: reviewStatus inválido")
    if entry.get("status") not in {"active", "deprecated"}:
        _add(issues, "schema", f"{path}: status inválido")
    if entry.get("criticality") not in {"standard", "important", "critical"}:
        _add(issues, "schema", f"{path}: criticality inválida")
    if entry.get("category") not in config["registryFiles"]:
        _add(issues, "schema", f"{path}: category inválida")
    responsibilities = entry.get("responsibilities")
    if isinstance(responsibilities, list) and not (1 <= len(responsibilities) <= config["maximumResponsibilities"]):
        _add(issues, "schema", f"{path}: responsibilities deve conter de 1 a {config['maximumResponsibilities']} itens")
    last_modified_at = entry.get("lastModifiedAt")
    if isinstance(last_modified_at, str):
        try:
            datetime.fromisoformat(last_modified_at.replace("Z", "+00:00"))
        except ValueError:
            _add(issues, "schema", f"{path}: lastModifiedAt invÃ¡lida")
    timestamp = entry.get("lastScannedAt")
    if isinstance(timestamp, str):
        try:
            datetime.fromisoformat(timestamp.replace("Z", "+00:00"))
        except ValueError:
            _add(issues, "schema", f"{path}: lastScannedAt não é uma data ISO válida")


def _validate_semantics(
    entry: dict[str, Any],
    registered_paths: set[str],
    config: dict[str, Any],
    issues: dict[str, list[str]],
) -> None:
    path = str(entry.get("path", "<sem caminho>"))
    description = str(entry.get("description", "")).strip()
    module = str(entry.get("module", "")).strip()
    if config.get("requireDescription") and not description:
        _add(issues, "classification", f"{path}: descrição vazia")
    if description and any(fragment in description.casefold() for fragment in GENERIC_DESCRIPTIONS):
        _add(issues, "classification", f"{path}: descrição genérica não identifica a função concreta")
    if config.get("requireModule") and not module:
        _add(issues, "classification", f"{path}: módulo vazio")
    if entry.get("category") == "uncategorized":
        _add(issues, "classification", f"{path}: arquivo novo sem classificação")

    if path and module:
        expected = module_for(path)
        actual_owner = module.split("/", 1)[0]
        expected_owner = expected.split("/", 1)[0]
        if actual_owner != expected_owner:
            _add(
                issues,
                "classification",
                f"{path}: módulo '{module}' conflita com o proprietário detectável '{expected}'",
            )
    for field in ("relatedFiles", "probableRelatedFiles"):
        for related in entry.get(field, []) if isinstance(entry.get(field), list) else []:
            if related not in registered_paths:
                _add(issues, "classification", f"{path}: {field} aponta para arquivo não registrado: {related}")


def _validate_config(root: Path, config: dict[str, Any], issues: dict[str, list[str]]) -> None:
    for key in ("excludeDirectoryNames", "excludePathPrefixes", "excludeFiles"):
        value = config.get(key)
        if not isinstance(value, dict):
            _add(issues, "exclusions", f"config.json: {key} deve mapear regras para justificativas")
            continue
        for rule, reason in value.items():
            if not isinstance(rule, str) or not rule.strip() or not isinstance(reason, str) or not reason.strip():
                _add(issues, "exclusions", f"config.json: regra sem justificativa em {key}: {rule!r}")
    include_reasons = config.get("explicitIncludeReasons", {})
    for relative_path in config.get("explicitIncludeFiles", []):
        if not (root / relative_path).is_file():
            _add(issues, "exclusions", f"Inclusão explícita inexistente: {relative_path}")
        if not str(include_reasons.get(relative_path, "")).strip():
            _add(issues, "exclusions", f"Inclusão explícita sem justificativa: {relative_path}")
        if relative_path in config.get("excludeFiles", {}):
            _add(issues, "exclusions", f"Arquivo incluído e excluído simultaneamente: {relative_path}")
    for relative_path in config.get("excludeFiles", {}):
        if relative_path.startswith(("app/src/", "app/scripts/", "app/tests/", "tools/", "installer/", "Code Registry/tools/")):
            _add(issues, "exclusions", f"Exclusão suspeita dentro de código mantido: {relative_path}")
    for scan_root in config.get("scanRoots", []):
        if not (root / scan_root).is_dir():
            _add(issues, "exclusions", f"Raiz de scan inexistente: {scan_root}")


def _validate_generated(
    root: Path,
    entries: list[dict[str, Any]],
    config: dict[str, Any],
    issues: dict[str, list[str]],
) -> None:
    generated_root = root / "Code Registry" / "generated"
    expectations = {"code-registry.json": ("entries", entries)}
    for filename, (field, expected) in expectations.items():
        path = generated_root / filename
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
            _add(issues, "generatedConflicts", f"{filename}: inválido ou ausente: {exc}")
            continue
        if payload.get(field) != expected:
            _add(issues, "generatedConflicts", f"{filename}: conflito com os registros modulares")
        if filename == "code-registry.json" and payload.get("entryCount") != len(entries):
            _add(issues, "generatedConflicts", f"{filename}: entryCount inconsistente")

    site_path = generated_root / "site-data.json"
    try:
        site_data = json.loads(site_path.read_text(encoding="utf-8"))
        expected_explorer = build_explorer_payload(root, entries)
    except (OSError, UnicodeDecodeError, json.JSONDecodeError, ValueError) as exc:
        _add(issues, "generatedConflicts", f"site-data.json: inválido ou ausente: {exc}")
        return
    for field, expected in expected_explorer.items():
        if site_data.get(field) != expected:
            _add(issues, "generatedConflicts", f"site-data.json: projeção '{field}' conflita com os registros")
    if config.get("requireProductAreaCoverage"):
        for path in expected_explorer["productAreaCoverage"]["unmappedPaths"]:
            _add(issues, "classification", f"{path}: arquivo sem Product Area no explorer.json")


def validate_registry(
    root: Path,
    config: dict[str, Any],
    *,
    persist_audit: bool = False,
) -> dict[str, Any]:
    issue_names = (
        "missing", "orphaned", "pending", "schema", "duplicates", "hashes",
        "classification", "exclusions", "generatedConflicts",
    )
    issues: dict[str, list[str]] = {name: [] for name in issue_names}
    _validate_config(root, config, issues)
    try:
        explorer_config = load_explorer_config(root)
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        _add(issues, "schema", f"explorer.json inválido: {exc}")
    else:
        for error in validate_explorer_config(explorer_config):
            _add(issues, "schema", error)

    schema_path = root / "Code Registry" / "registry.schema.json"
    try:
        schema = json.loads(schema_path.read_text(encoding="utf-8"))
        required = set(schema["required"])
        allowed = set(schema["properties"])
    except (OSError, UnicodeDecodeError, json.JSONDecodeError, KeyError, TypeError) as exc:
        schema = {}
        required = set()
        allowed = set()
        _add(issues, "schema", f"registry.schema.json inválido: {exc}")

    entries, load_errors = load_registry_entries(root, config)
    for error in load_errors:
        _add(issues, "schema", error)
    paths = [str(entry.get("path", "")) for entry in entries]
    registered_paths = set(paths)
    for path in sorted({path for path in paths if paths.count(path) > 1}, key=str.casefold):
        _add(issues, "duplicates", f"Caminho duplicado: {path}")

    discovered = discover_files(root, config)
    discovered_paths = set(discovered)
    for path in sorted(discovered_paths - registered_paths, key=str.casefold):
        _add(issues, "missing", path)
    for path in sorted(registered_paths - discovered_paths, key=str.casefold):
        _add(issues, "orphaned", path)

    for entry in entries:
        _validate_schema_entry(entry, required, allowed, config, issues)
        _validate_semantics(entry, registered_paths, config, issues)
        path_text = str(entry.get("path", ""))
        path = discovered.get(path_text)
        if path is None:
            continue
        expected_category = category_for(path_text, path, config)
        if entry.get("category") != expected_category:
            _add(
                issues,
                "classification",
                f"{path_text}: categoria '{entry.get('category')}' deveria ser '{expected_category}'",
            )
        try:
            current = analyze_file(path, path_text)
        except (OSError, UnicodeError) as exc:
            _add(issues, "hashes", f"{path_text}: falha ao analisar: {exc}")
            continue
        for field in (
            "filename", "extension", "language", "kind", "dependencies", "includes", "imports",
            "exports", "symbols", "lineCount", "sizeBytes", "lastModifiedAt", "hash",
        ):
            if entry.get(field) != current.get(field):
                _add(issues, "hashes", f"{path_text}: dado automático inconsistente: {field}")
        if (
            entry.get("reviewStatus") != "reviewed"
            or entry.get("hash") != entry.get("lastReviewedHash")
            or current.get("hash") != entry.get("hash")
        ):
            _add(issues, "pending", path_text)

    _validate_generated(root, entries, config, issues)
    for values in issues.values():
        values.sort(key=str.casefold)

    try:
        previous_audit = json.loads(
            (root / "Code Registry" / "generated" / "audit-summary.json").read_text(encoding="utf-8")
        )
        events = previous_audit.get("events")
    except (OSError, UnicodeDecodeError, json.JSONDecodeError):
        events = None
    audit = base_audit(entries, events)
    error_count = sum(len(values) for values in issues.values())
    audit["status"] = "approved" if error_count == 0 else "failed"
    audit["counts"] = {
        "found": len(discovered_paths),
        "registered": len(entries),
        "missing": len(issues["missing"]),
        "orphaned": len(issues["orphaned"]),
        "pending": len(issues["pending"]),
        "errors": error_count,
    }
    audit["issues"] = issues
    if persist_audit:
        write_validation_results(root, audit)
    return audit
