#!/usr/bin/env python3
"""Build bounded context packs and explainable local retrieval for AI clients."""

from __future__ import annotations

import re
from collections import defaultdict
from pathlib import Path
from typing import Any

from history_store import recent_changes, search_local
from registry_builder import load_registry_entries
from semantic_index import search_semantic


def _entries(root: Path, config: dict[str, Any]) -> list[dict[str, Any]]:
    entries, errors = load_registry_entries(root, config)
    if errors:
        raise RuntimeError("; ".join(errors))
    return entries


def _source_slice(root: Path, path: str, maximum_chars: int) -> str:
    payload = (root / path).read_bytes()
    if len(payload) > 2 * 1024 * 1024:
        return "[Source omitted: exceeds reader limit.]"
    text = payload.decode("utf-8", errors="replace")
    return text[:maximum_chars] + ("\n[Source truncated by context budget.]" if len(text) > maximum_chars else "")


def _aliases(config: dict[str, Any], query: str) -> list[tuple[str, str]]:
    """Return explicit query expansions with a human-readable reason for each."""
    configured = config.get("retrieval", {}).get("aliases", {})
    values: list[tuple[str, str]] = [(query, "Original query")]
    words = re.findall(r"[\w-]+", query.casefold())
    for word in words:
        for alias in configured.get(word, []):
            values.append((str(alias), f"Alias: {word} -> {alias}"))
    unique: list[tuple[str, str]] = []
    seen: set[str] = set()
    for term, reason in values:
        key = term.casefold()
        if key not in seen:
            seen.add(key)
            unique.append((term, reason))
    return unique


def _bounded(value: int | None, config: dict[str, Any]) -> int:
    maximum = value or int(config.get("context", {}).get("defaultCharacterBudget", 16000))
    return max(1000, min(maximum, int(config.get("context", {}).get("maximumCharacterBudget", 60000))))


def file_context(root: Path, config: dict[str, Any], path: str, *, budget: int | None = None, include_source: bool = True) -> dict[str, Any]:
    normalized = path.replace("\\", "/").removeprefix("./")
    entries = _entries(root, config)
    entry = next((item for item in entries if item["path"] == normalized), None)
    if entry is None:
        raise ValueError("Path is not a registered source file.")
    maximum = _bounded(budget, config)
    related = [item for item in entries if item["path"] in entry.get("relatedFiles", [])]
    metadata = {key: entry.get(key) for key in ("path", "filename", "module", "description", "responsibilities", "criticality", "language", "kind", "symbols", "relatedFiles", "lastModifiedAt", "hash")}
    pack: dict[str, Any] = {
        "schemaVersion": 1, "source": "Code Registry local snapshot", "budget": maximum,
        "file": metadata,
        "related": [{"path": item["path"], "description": item["description"], "module": item["module"]} for item in related],
        "recentChanges": recent_changes(root, config, limit=10, path=normalized),
    }
    used = len(str(pack))
    if include_source and used < maximum:
        pack["sourceText"] = _source_slice(root, normalized, maximum - used)
    pack["truncated"] = len(str(pack)) > maximum
    return pack


def module_context(root: Path, config: dict[str, Any], module: str, *, budget: int | None = None) -> dict[str, Any]:
    entries = [entry for entry in _entries(root, config) if module.casefold() in entry.get("module", "").casefold()]
    if not entries:
        raise ValueError("No registered module matches this query.")
    maximum = _bounded(budget, config)
    result: dict[str, Any] = {"schemaVersion": 1, "moduleQuery": module, "files": [], "budget": maximum}
    for entry in entries:
        candidate = {"path": entry["path"], "description": entry["description"], "responsibilities": entry["responsibilities"], "criticality": entry["criticality"], "symbols": entry.get("symbols", {})}
        if len(str(result)) + len(str(candidate)) > maximum:
            result["truncated"] = True
            break
        result["files"].append(candidate)
    result["recentChanges"] = [change for change in recent_changes(root, config, limit=50) if any(change["path"] == item["path"] for item in result["files"])]
    return result


def search_context(root: Path, config: dict[str, Any], query: str, *, limit: int = 12) -> dict[str, Any]:
    """Merge FTS searches into ranked, explainable results without leaking BM25 internals."""
    grouped: dict[str, dict[str, Any]] = {}
    for term, reason in _aliases(config, query):
        for fts_rank, item in enumerate(search_local(root, config, term, limit=max(limit * 2, 12)), start=1):
            candidate = grouped.setdefault(item["path"], {
                "path": item["path"], "module": item["module"], "language": item["language"],
                "excerpt": item["excerpt"], "matchReasons": [], "_relevance": 0.0, "_ftsRank": fts_rank,
            })
            candidate["matchReasons"].append(reason)
            candidate["_relevance"] = max(candidate["_relevance"], -float(item["score"]))
            candidate["_ftsRank"] = min(candidate["_ftsRank"], fts_rank)
    semantic = search_semantic(root, config, query, limit=max(limit * 3, 30))
    semantic_ranks = {item["path"]: rank for rank, item in enumerate(semantic.get("results", []), start=1)}
    if grouped or semantic_ranks:
        entries_by_path = {entry["path"]: entry for entry in _entries(root, config)}
        for path, semantic_rank in semantic_ranks.items():
            entry = entries_by_path[path]
            candidate = grouped.setdefault(path, {"path": path, "module": entry["module"], "language": entry["language"], "excerpt": entry["description"], "matchReasons": [], "_relevance": 0.0, "_ftsRank": 999})
            candidate["matchReasons"].append("Semantic intent match")
            candidate["_semanticRank"] = semantic_rank
        for candidate in grouped.values():
            candidate["_rrf"] = (1 / (60 + candidate["_ftsRank"])) + (1 / (60 + candidate.get("_semanticRank", 999)))
        ordered = sorted(grouped.values(), key=lambda item: (-item["_rrf"], -len(set(item["matchReasons"])), -item["_relevance"], item["path"].casefold()))[:max(1, min(limit, 50))]
        results = []
        for rank, item in enumerate(ordered, start=1):
            item.pop("_relevance")
            item.pop("_semanticRank", None)
            item.pop("_ftsRank", None)
            item.pop("_rrf", None)
            item["rank"] = rank
            item["matchReasons"] = list(dict.fromkeys(item["matchReasons"]))
            results.append(item)
        return {"engine": "local-hybrid-fts5-semantic" if semantic.get("available") else "local-fts5-expanded", "semantic": semantic, "query": query, "expansions": [{"term": term, "reason": reason} for term, reason in _aliases(config, query)], "results": results}

    terms = query.casefold().split()
    results = []
    for entry in _entries(root, config):
        text = " ".join([entry["path"], entry["module"], entry["description"], *entry["responsibilities"]]).casefold()
        matched = [term for term in terms if term in text]
        if matched:
            results.append({"path": entry["path"], "module": entry["module"], "language": entry["language"], "excerpt": entry["description"], "matchReasons": [f"Fallback term: {term}" for term in matched]})
    ordered = sorted(results, key=lambda item: (-len(item["matchReasons"]), item["path"].casefold()))[:limit]
    for rank, item in enumerate(ordered, start=1):
        item["rank"] = rank
    return {"engine": "registry-fallback", "query": query, "expansions": [], "results": ordered}


def locate_ui_surface(root: Path, config: dict[str, Any], surface: str, *, limit: int = 8) -> dict[str, Any]:
    """Trace a user-facing UI concept to likely label, owner, renderer, and theme files."""
    search = search_context(root, config, surface, limit=limit)
    evidence = []
    for item in search["results"]:
        if item["path"].startswith("Code Registry/tools/tests/") or "/tests/" in item["path"]:
            continue
        if not (item["path"].startswith("app/src/ui/") or item["path"].startswith("app/runtime_assets/.resources/web-control/")):
            continue
        try:
            text = (root / item["path"]).read_text(encoding="utf-8", errors="replace")[:131072]
        except OSError:
            continue
        roles = []
        if re.search(r'"[^"\n]*' + re.escape(surface) + r'[^"\n]*"', text, re.IGNORECASE):
            roles.append("visible label")
        if "WorkspaceMode" in text:
            roles.append("workspace routing")
        if re.search(r"draw[A-Z]|drawGrid", text):
            roles.append("renderer")
        if "theme." in text or "Theme" in text:
            roles.append("theme token consumer")
        confidence = len(roles)
        if "/ui/" in item["path"]:
            confidence += 3
        evidence.append({**item, "surfaceRoles": roles or ["related source"], "_uiConfidence": confidence})
    evidence.sort(key=lambda item: (-item.pop("_uiConfidence"), item["rank"], item["path"].casefold()))
    for rank, item in enumerate(evidence, start=1):
        item["rank"] = rank
    return {
        "surface": surface, "searchEngine": search["engine"], "expansions": search["expansions"],
        "candidates": evidence,
        "recommendedNextStep": "Use read_symbol on a renderer candidate before changing visual behavior.",
    }


def read_symbol(root: Path, config: dict[str, Any], path: str, symbol: str, *, maximum_chars: int = 16000) -> dict[str, Any]:
    """Read one C/C++/Python-style function body with line provenance and a hard budget."""
    normalized = path.replace("\\", "/").removeprefix("./")
    if normalized not in {entry["path"] for entry in _entries(root, config)}:
        raise ValueError("Path is not a registered source file.")
    lines = (root / normalized).read_text(encoding="utf-8", errors="replace").splitlines()
    pattern = re.compile(re.escape(symbol) + r"\s*\(")
    start = next((index for index, line in enumerate(lines) if pattern.search(line)), None)
    if start is None:
        raise ValueError("Symbol was not found in the registered source file.")
    brace_depth = 0
    seen_body = False
    end = start
    for index in range(start, len(lines)):
        brace_depth += lines[index].count("{") - lines[index].count("}")
        seen_body = seen_body or "{" in lines[index]
        end = index
        if seen_body and brace_depth <= 0:
            break
    content = "\n".join(lines[start:end + 1])
    clipped = content[:max(1000, min(maximum_chars, 60000))]
    return {"path": normalized, "symbol": symbol, "startLine": start + 1, "endLine": end + 1, "content": clipped, "truncated": len(clipped) < len(content)}
