#!/usr/bin/env python3
"""Measure Code Registry retrieval against a simple direct lexical baseline."""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any

from context_builder import locate_ui_surface, search_context
from registry_builder import load_registry_entries
from scanner import load_config


ROOT = Path(__file__).resolve().parents[2]
DEFAULT_FIXTURE = ROOT / "Code Registry" / "retrieval-evaluation.json"


def _baseline(root: Path, entries: list[dict[str, Any]], query: str, limit: int) -> list[str]:
    terms = [term.casefold() for term in re.findall(r"[\w-]+", query) if len(term) > 2]
    ranked: list[tuple[int, str]] = []
    for entry in entries:
        try:
            text = (root / entry["path"]).read_text(encoding="utf-8", errors="replace").casefold()
        except OSError:
            continue
        score = sum(text.count(term) for term in terms)
        if score:
            ranked.append((score, entry["path"]))
    return [path for _, path in sorted(ranked, key=lambda item: (-item[0], item[1].casefold()))[:limit]]


def _rank(paths: list[str], expected: str) -> int | None:
    try:
        return paths.index(expected) + 1
    except ValueError:
        return None


def evaluate(root: Path, fixture_path: Path) -> dict[str, Any]:
    fixture = json.loads(fixture_path.read_text(encoding="utf-8"))
    config = load_config(root)
    entries, errors = load_registry_entries(root, config)
    if errors:
        raise RuntimeError("; ".join(errors))
    cases = []
    for item in fixture["cases"]:
        query, expected = item["query"], item["expectedPath"]
        if item.get("kind") == "ui":
            registry_paths = [candidate["path"] for candidate in locate_ui_surface(root, config, query, limit=12)["candidates"]]
        else:
            registry_paths = [candidate["path"] for candidate in search_context(root, config, query, limit=12)["results"]]
        baseline_paths = _baseline(root, entries, query, 12)
        cases.append({
            "id": item["id"], "query": query, "expectedPath": expected,
            "registryRank": _rank(registry_paths, expected), "baselineRank": _rank(baseline_paths, expected),
        })
    def metrics(field: str) -> dict[str, int]:
        return {"top1": sum(case[field] == 1 for case in cases), "top3": sum((case[field] or 99) <= 3 for case in cases), "found": sum(case[field] is not None for case in cases)}
    return {"caseCount": len(cases), "registry": metrics("registryRank"), "baseline": metrics("baselineRank"), "cases": cases}


def main() -> int:
    fixture = Path(sys.argv[1]) if len(sys.argv) > 1 else DEFAULT_FIXTURE
    print(json.dumps(evaluate(ROOT, fixture), ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
