#!/usr/bin/env python3
"""Deterministic JSON persistence and generated payloads for Code Registry."""

from __future__ import annotations

import json
import os
import shutil
import tempfile
from collections import Counter
from pathlib import Path
from typing import Any, Iterable

from explorer_builder import build_explorer_payload


ENTRY_FIELD_ORDER = (
    "path", "filename", "extension", "language", "kind", "category", "module",
    "description", "responsibilities", "criticality", "relatedFiles",
    "probableRelatedFiles", "previousPaths", "dependencies", "includes", "imports",
    "exports", "symbols", "lineCount", "sizeBytes", "lastModifiedAt", "hash", "lastReviewedHash",
    "reviewStatus", "status", "lastScannedAt",
)
FAMILY_ORDER = ("C/C++", "Web", "Python", "Shaders", "Build")


def _json_bytes(data: Any) -> bytes:
    return (json.dumps(data, ensure_ascii=False, indent=2) + "\n").encode("utf-8")


def atomic_write_json(path: Path, data: Any) -> None:
    """Replace a JSON file atomically, using a temporary rollback copy when it exists."""
    path.parent.mkdir(parents=True, exist_ok=True)
    backup: Path | None = None
    temporary: Path | None = None
    try:
        if path.exists():
            handle, backup_name = tempfile.mkstemp(prefix=f".{path.name}.backup.", dir=path.parent)
            os.close(handle)
            backup = Path(backup_name)
            shutil.copyfile(path, backup)

        handle, temporary_name = tempfile.mkstemp(prefix=f".{path.name}.write.", dir=path.parent)
        temporary = Path(temporary_name)
        with os.fdopen(handle, "wb") as stream:
            stream.write(_json_bytes(data))
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
        temporary = None
    except Exception:
        if backup is not None and backup.exists():
            os.replace(backup, path)
            backup = None
        raise
    finally:
        if temporary is not None:
            temporary.unlink(missing_ok=True)
        if backup is not None:
            backup.unlink(missing_ok=True)


def ordered_entry(entry: dict[str, Any]) -> dict[str, Any]:
    ordered = {field: entry[field] for field in ENTRY_FIELD_ORDER if field in entry}
    for field in sorted(set(entry) - set(ENTRY_FIELD_ORDER)):
        ordered[field] = entry[field]
    return ordered


def load_registry_entries(root: Path, config: dict[str, Any]) -> tuple[list[dict[str, Any]], list[str]]:
    entries: list[dict[str, Any]] = []
    errors: list[str] = []
    for category, relative_path in config["registryFiles"].items():
        path = root / "Code Registry" / relative_path
        if not path.exists():
            errors.append(f"Registro modular ausente: {path.relative_to(root).as_posix()}")
            continue
        try:
            payload = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
            errors.append(f"JSON inválido em {path.relative_to(root).as_posix()}: {exc}")
            continue
        if not isinstance(payload, dict) or not isinstance(payload.get("entries"), list):
            errors.append(f"Formato modular inválido: {path.relative_to(root).as_posix()}")
            continue
        if payload.get("category") != category:
            errors.append(f"Categoria modular incorreta em {path.relative_to(root).as_posix()}")
        for item in payload["entries"]:
            if not isinstance(item, dict):
                errors.append(f"Entrada não-objeto em {path.relative_to(root).as_posix()}")
                continue
            if item.get("category") != category:
                errors.append(
                    f"Entrada {item.get('path', '<sem caminho>')} está no registro modular {category}, "
                    f"mas declara categoria {item.get('category', '<ausente>')}"
                )
            entries.append(item)
    return sorted(entries, key=lambda item: str(item.get("path", "")).casefold()), errors


def write_modular_registries(root: Path, config: dict[str, Any], entries: Iterable[dict[str, Any]]) -> None:
    grouped: dict[str, list[dict[str, Any]]] = {category: [] for category in config["registryFiles"]}
    for entry in entries:
        grouped.setdefault(entry["category"], []).append(ordered_entry(entry))
    for category, relative_path in config["registryFiles"].items():
        payload = {
            "schemaVersion": config["schemaVersion"],
            "category": category,
            "entries": sorted(grouped.get(category, []), key=lambda item: item["path"].casefold()),
        }
        atomic_write_json(root / "Code Registry" / relative_path, payload)


def family_for(entry: dict[str, Any]) -> str:
    category = entry.get("category")
    language = entry.get("language")
    if category == "build-scripts":
        return "Build"
    if category == "shaders":
        return "Shaders"
    if language == "Python":
        return "Python"
    if language in {"C", "C++", "Objective-C", "Objective-C++"}:
        return "C/C++"
    if language in {
        "HTML", "CSS", "SCSS", "Sass", "Less", "JavaScript", "JavaScript React",
        "TypeScript", "TypeScript React",
    }:
        return "Web"
    return "Build"


def _counts(entries: list[dict[str, Any]]) -> dict[str, Any]:
    families = Counter(family_for(entry) for entry in entries)
    languages = Counter(str(entry["language"]) for entry in entries)
    modules = Counter(str(entry["module"]) for entry in entries)
    statuses = Counter(str(entry["status"]) for entry in entries)
    reviews = Counter(str(entry["reviewStatus"]) for entry in entries)
    return {
        "total": len(entries),
        "lines": sum(int(entry.get("lineCount", 0)) for entry in entries),
        "families": {name: families.get(name, 0) for name in FAMILY_ORDER},
        "languages": dict(sorted(languages.items(), key=lambda item: item[0].casefold())),
        "modules": dict(sorted(modules.items(), key=lambda item: item[0].casefold())),
        "statuses": dict(sorted(statuses.items())),
        "reviewStatuses": dict(sorted(reviews.items())),
    }


def _generated_at(entries: list[dict[str, Any]]) -> str:
    timestamps = [str(entry.get("lastScannedAt", "")) for entry in entries if entry.get("lastScannedAt")]
    return max(timestamps, default="")


def base_audit(entries: list[dict[str, Any]], events: dict[str, Any] | None = None) -> dict[str, Any]:
    pending = [entry["path"] for entry in entries if entry.get("reviewStatus") != "reviewed"]
    counts = _counts(entries)
    return {
        "schemaVersion": 1,
        "generatedAt": _generated_at(entries),
        "status": "approved" if not pending else "failed",
        "counts": {
            "found": len(entries),
            "registered": len(entries),
            "missing": 0,
            "orphaned": 0,
            "pending": len(pending),
            "errors": len(pending),
            "lines": counts["lines"],
        },
        "families": counts["families"],
        "languages": counts["languages"],
        "modules": counts["modules"],
        "issues": {
            "missing": [],
            "orphaned": [],
            "pending": pending,
            "schema": [],
            "duplicates": [],
            "hashes": [],
            "classification": [],
            "exclusions": [],
            "generatedConflicts": [],
        },
        "events": events or {"new": [], "changed": [], "moved": [], "removed": []},
    }


def build_outputs(
    root: Path,
    config: dict[str, Any],
    entries: list[dict[str, Any]],
    events: dict[str, Any] | None = None,
) -> dict[str, Any]:
    entries = [ordered_entry(entry) for entry in sorted(entries, key=lambda item: item["path"].casefold())]
    generated_at = _generated_at(entries)
    generated_root = root / "Code Registry" / "generated"
    previous_audit_path = generated_root / "audit-summary.json"
    if events is None and previous_audit_path.exists():
        try:
            previous = json.loads(previous_audit_path.read_text(encoding="utf-8"))
            events = previous.get("events")
        except (OSError, UnicodeDecodeError, json.JSONDecodeError):
            events = None
    audit = base_audit(entries, events)
    counts = _counts(entries)
    consolidated = {
        "schemaVersion": config["schemaVersion"],
        "generatedAt": generated_at,
        "entryCount": len(entries),
        "entries": entries,
    }
    explorer = build_explorer_payload(root, entries)
    site_data = {
        "schemaVersion": config["schemaVersion"],
        "generatedAt": generated_at,
        **explorer,
        "filters": {
            "languages": list(counts["languages"]),
            "modules": list(counts["modules"]),
            "statuses": list(counts["statuses"]),
            "reviewStatuses": list(counts["reviewStatuses"]),
            "roles": [role["id"] for role in explorer["roles"]],
            "productAreas": [area["id"] for area in explorer["productAreas"]],
        },
        "counts": counts,
        "audit": audit,
    }
    atomic_write_json(generated_root / "code-registry.json", consolidated)
    atomic_write_json(generated_root / "audit-summary.json", audit)
    atomic_write_json(generated_root / "site-data.json", site_data)
    return audit


def write_validation_results(root: Path, audit: dict[str, Any]) -> None:
    generated_root = root / "Code Registry" / "generated"
    atomic_write_json(generated_root / "audit-summary.json", audit)
    site_path = generated_root / "site-data.json"
    try:
        site_data = json.loads(site_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError):
        return
    site_data["audit"] = audit
    atomic_write_json(site_path, site_data)
