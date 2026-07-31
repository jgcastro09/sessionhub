#!/usr/bin/env python3
"""Build deterministic architecture, product-area, logical-unit, and graph projections."""

from __future__ import annotations

import fnmatch
import hashlib
import json
from collections import Counter, defaultdict
from pathlib import Path, PurePosixPath
from typing import Any, Iterable


HEADER_EXTENSIONS = {".h", ".hh", ".hpp", ".hxx"}
IMPLEMENTATION_EXTENSIONS = {".c", ".cc", ".cpp", ".cxx", ".m", ".mm"}
ROLE_PRIORITY = (
    "test", "build", "tool", "platform", "output", "render", "persistence",
    "service", "ui", "state", "node", "coordinator", "implementation",
)
CRITICALITY_PRIORITY = {"standard": 0, "important": 1, "critical": 2}


def load_explorer_config(root: Path) -> dict[str, Any]:
    path = root / "Code Registry" / "explorer.json"
    return json.loads(path.read_text(encoding="utf-8"))


def _stable_id(prefix: str, value: str) -> str:
    digest = hashlib.sha256(value.encode("utf-8")).hexdigest()[:16]
    return f"{prefix}:{digest}"


def _natural_key(value: str) -> tuple[str, ...]:
    return tuple(part.casefold() for part in value.split("/"))


def _rule_matches(entry: dict[str, Any], rule: dict[str, Any]) -> bool:
    path = str(entry["path"])
    module = str(entry["module"])
    folded_path = path.casefold()
    folded_module = module.casefold()
    positives: list[bool] = []

    exact_paths = {str(value).casefold() for value in rule.get("exactPaths", [])}
    if exact_paths:
        positives.append(folded_path in exact_paths)
    modules = {str(value).casefold() for value in rule.get("modules", [])}
    if modules:
        positives.append(folded_module in modules)
    for key, value in (("pathPrefixes", folded_path), ("modulePrefixes", folded_module)):
        prefixes = [str(item).casefold().rstrip("/") for item in rule.get(key, [])]
        if prefixes:
            positives.append(any(value == prefix or value.startswith(prefix + "/") for prefix in prefixes))
    patterns = [str(value).casefold() for value in rule.get("pathGlobs", [])]
    if patterns:
        positives.append(any(fnmatch.fnmatchcase(folded_path, pattern) for pattern in patterns))
    terms = [str(value).casefold() for value in rule.get("textTerms", [])]
    if terms:
        symbols = entry.get("symbols", {})
        symbol_values = [item for values in symbols.values() for item in values]
        searchable = "\n".join(
            [
                path,
                module,
                str(entry.get("description", "")),
                *entry.get("responsibilities", []),
                *symbol_values,
            ]
        ).casefold()
        positives.append(any(term in searchable for term in terms))

    excluded = {str(value).casefold() for value in rule.get("excludePaths", [])}
    if folded_path in excluded:
        return False
    excluded_globs = [str(value).casefold() for value in rule.get("excludePathGlobs", [])]
    if any(fnmatch.fnmatchcase(folded_path, pattern) for pattern in excluded_globs):
        return False
    return any(positives)


def _validate_match_rule(rule: Any, location: str, errors: list[str]) -> None:
    if not isinstance(rule, dict):
        errors.append(f"{location}: match deve ser um objeto")
        return
    allowed = {
        "exactPaths", "modules", "pathPrefixes", "modulePrefixes", "pathGlobs",
        "textTerms", "excludePaths", "excludePathGlobs",
    }
    unknown = sorted(set(rule) - allowed)
    if unknown:
        errors.append(f"{location}: regras desconhecidas: {', '.join(unknown)}")
    if not any(key in rule for key in allowed - {"excludePaths", "excludePathGlobs"}):
        errors.append(f"{location}: match não possui regra positiva")
    for key, values in rule.items():
        if not isinstance(values, list) or any(not isinstance(value, str) or not value.strip() for value in values):
            errors.append(f"{location}.{key}: deve ser uma lista de strings não vazias")


def validate_explorer_config(config: Any) -> list[str]:
    errors: list[str] = []
    if not isinstance(config, dict):
        return ["explorer.json: raiz deve ser um objeto"]
    if config.get("schemaVersion") != 1:
        errors.append("explorer.json: schemaVersion deve ser 1")
    graph = config.get("graph")
    if not isinstance(graph, dict):
        errors.append("explorer.json: graph deve ser um objeto")
    else:
        for key in ("initialNodeLimit", "maximumNodeLimit", "defaultDepth"):
            value = graph.get(key)
            if not isinstance(value, int) or isinstance(value, bool) or value < 1:
                errors.append(f"explorer.json: graph.{key} deve ser inteiro positivo")
        if isinstance(graph.get("initialNodeLimit"), int) and isinstance(graph.get("maximumNodeLimit"), int):
            if graph["initialNodeLimit"] > graph["maximumNodeLimit"]:
                errors.append("explorer.json: initialNodeLimit excede maximumNodeLimit")

    roles = config.get("roles")
    if not isinstance(roles, list) or not roles:
        errors.append("explorer.json: roles deve ser uma lista não vazia")
    else:
        seen_roles: set[str] = set()
        for index, role in enumerate(roles):
            location = f"explorer.json.roles[{index}]"
            if not isinstance(role, dict):
                errors.append(f"{location}: deve ser um objeto")
                continue
            role_id = role.get("id")
            if not isinstance(role_id, str) or not role_id.strip():
                errors.append(f"{location}: id ausente")
            elif role_id in seen_roles:
                errors.append(f"{location}: id duplicado '{role_id}'")
            else:
                seen_roles.add(role_id)
            if not isinstance(role.get("label"), str) or not role["label"].strip():
                errors.append(f"{location}: label ausente")
            _validate_match_rule(role.get("match"), f"{location}.match", errors)

    areas = config.get("productAreas")
    if not isinstance(areas, list) or not areas:
        errors.append("explorer.json: productAreas deve ser uma lista não vazia")
        return errors
    seen_areas: set[str] = set()

    def visit(area: Any, location: str) -> None:
        if not isinstance(area, dict):
            errors.append(f"{location}: deve ser um objeto")
            return
        area_id = area.get("id")
        if not isinstance(area_id, str) or not area_id.strip():
            errors.append(f"{location}: id ausente")
        elif area_id in seen_areas:
            errors.append(f"{location}: id duplicado '{area_id}'")
        else:
            seen_areas.add(area_id)
        for key in ("label", "description", "color"):
            if not isinstance(area.get(key), str) or not area[key].strip():
                errors.append(f"{location}: {key} ausente")
        if "match" in area:
            _validate_match_rule(area["match"], f"{location}.match", errors)
        children = area.get("children", [])
        if not isinstance(children, list):
            errors.append(f"{location}.children: deve ser uma lista")
            return
        for index, child in enumerate(children):
            visit(child, f"{location}.children[{index}]")

    for index, area in enumerate(areas):
        visit(area, f"explorer.json.productAreas[{index}]")
    return errors


def _role_for(entry: dict[str, Any], config: dict[str, Any]) -> str:
    for role in config["roles"]:
        if _rule_matches(entry, role["match"]):
            return str(role["id"])
    return "implementation"


def _unit_key(entry: dict[str, Any]) -> str:
    path = PurePosixPath(str(entry["path"]))
    extension = str(entry["extension"]).casefold()
    if extension in HEADER_EXTENSIONS | IMPLEMENTATION_EXTENSIONS:
        return str(path.with_suffix(""))
    return str(path)


def _primary_entry(entries: list[dict[str, Any]]) -> dict[str, Any]:
    order = {extension: index for index, extension in enumerate(
        (".cpp", ".mm", ".m", ".cc", ".cxx", ".c", ".hpp", ".h", ".hh", ".hxx")
    )}
    return min(entries, key=lambda entry: (order.get(str(entry["extension"]).casefold(), 99), entry["path"].casefold()))


def _highest_criticality(entries: Iterable[dict[str, Any]]) -> str:
    return max(
        (str(entry.get("criticality", "standard")) for entry in entries),
        key=lambda value: CRITICALITY_PRIORITY.get(value, 0),
        default="standard",
    )


def _build_logical_units(entries: list[dict[str, Any]], config: dict[str, Any]) -> tuple[list[dict[str, Any]], dict[str, str]]:
    grouped: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for entry in entries:
        grouped[_unit_key(entry)].append(entry)
    units: list[dict[str, Any]] = []
    file_to_unit: dict[str, str] = {}
    for key in sorted(grouped, key=_natural_key):
        members = sorted(grouped[key], key=lambda entry: entry["path"].casefold())
        primary = _primary_entry(members)
        roles = Counter(_role_for(entry, config) for entry in members)
        role = min(
            roles,
            key=lambda value: (-roles[value], ROLE_PRIORITY.index(value) if value in ROLE_PRIORITY else 999, value),
        )
        unit_id = _stable_id("unit", key.casefold())
        for entry in members:
            file_to_unit[entry["path"]] = unit_id
        units.append(
            {
                "id": unit_id,
                "key": key,
                "label": PurePosixPath(key).name,
                "module": str(primary["module"]),
                "primaryPath": str(primary["path"]),
                "filePaths": [str(entry["path"]) for entry in members],
                "description": str(primary["description"]),
                "role": role,
                "language": str(primary["language"]),
                "kind": str(primary["kind"]),
                "criticality": _highest_criticality(members),
                "reviewStatus": "needs_review" if any(entry["reviewStatus"] != "reviewed" for entry in members) else "reviewed",
                "status": "deprecated" if all(entry["status"] == "deprecated" for entry in members) else "active",
                "lineCount": sum(int(entry["lineCount"]) for entry in members),
                "sizeBytes": sum(int(entry["sizeBytes"]) for entry in members),
            }
        )
    return units, file_to_unit


def _build_architecture(units: list[dict[str, Any]]) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    roots: dict[str, Any] = {}
    modules: dict[str, dict[str, Any]] = {}
    for unit in units:
        current_children = roots
        parts = str(unit["module"]).split("/")
        for index, part in enumerate(parts):
            path = "/".join(parts[: index + 1])
            node = current_children.setdefault(
                part,
                {"id": _stable_id("module", path.casefold()), "label": part, "path": path, "children": {}, "directUnitIds": []},
            )
            modules[path] = node
            current_children = node["children"]
        modules[str(unit["module"])]["directUnitIds"].append(unit["id"])

    def finalize(node: dict[str, Any]) -> dict[str, Any]:
        children = [finalize(child) for _, child in sorted(node["children"].items(), key=lambda item: item[0].casefold())]
        direct = sorted(node["directUnitIds"])
        all_units = sorted({*direct, *(unit_id for child in children for unit_id in child["unitIds"])})
        return {
            "id": node["id"],
            "label": node["label"],
            "path": node["path"],
            "directUnitIds": direct,
            "unitIds": all_units,
            "unitCount": len(all_units),
            "children": children,
        }

    tree = [finalize(node) for _, node in sorted(roots.items(), key=lambda item: item[0].casefold())]
    flat: list[dict[str, Any]] = []

    def flatten(node: dict[str, Any]) -> None:
        flat.append({key: value for key, value in node.items() if key != "children"})
        for child in node["children"]:
            flatten(child)

    for root_node in tree:
        flatten(root_node)
    return tree, sorted(flat, key=lambda item: item["path"].casefold())


def _summarize_paths(paths: set[str], entries_by_path: dict[str, dict[str, Any]], file_to_unit: dict[str, str]) -> dict[str, Any]:
    selected = [entries_by_path[path] for path in sorted(paths, key=str.casefold)]
    roles = Counter(str(entry["architecturalRole"]) for entry in selected)
    modules = Counter(str(entry["module"]) for entry in selected)
    languages = Counter(str(entry["language"]) for entry in selected)
    return {
        "filePaths": [entry["path"] for entry in selected],
        "unitIds": sorted({file_to_unit[entry["path"]] for entry in selected}),
        "counts": {
            "files": len(selected),
            "units": len({file_to_unit[entry["path"]] for entry in selected}),
            "pending": sum(entry["reviewStatus"] != "reviewed" for entry in selected),
            "roles": dict(sorted(roles.items())),
            "modules": dict(sorted(modules.items(), key=lambda item: item[0].casefold())),
            "languages": dict(sorted(languages.items(), key=lambda item: item[0].casefold())),
        },
    }


def _build_product_areas(
    entries: list[dict[str, Any]],
    config: dict[str, Any],
    file_to_unit: dict[str, str],
) -> tuple[list[dict[str, Any]], list[dict[str, Any]], dict[str, list[str]]]:
    entries_by_path = {entry["path"]: entry for entry in entries}
    file_areas: dict[str, list[str]] = defaultdict(list)
    flat: list[dict[str, Any]] = []

    def build(area: dict[str, Any], parent_id: str | None = None) -> tuple[dict[str, Any], set[str]]:
        direct = {
            entry["path"] for entry in entries
            if "match" in area and _rule_matches(entry, area["match"])
        }
        children: list[dict[str, Any]] = []
        aggregate = set(direct)
        for child in area.get("children", []):
            built_child, child_paths = build(child, str(area["id"]))
            children.append(built_child)
            aggregate.update(child_paths)
        for path in aggregate:
            file_areas[path].append(str(area["id"]))
        summary = _summarize_paths(aggregate, entries_by_path, file_to_unit)
        direct_summary = _summarize_paths(direct, entries_by_path, file_to_unit)
        built = {
            "id": str(area["id"]),
            "label": str(area["label"]),
            "description": str(area["description"]),
            "color": str(area["color"]),
            "parentId": parent_id,
            "directFilePaths": direct_summary["filePaths"],
            "directUnitIds": direct_summary["unitIds"],
            **summary,
            "children": children,
        }
        flat.append({key: value for key, value in built.items() if key != "children"})
        return built, aggregate

    tree = [build(area)[0] for area in config["productAreas"]]
    flat.sort(key=lambda item: item["label"].casefold())
    normalized_file_areas = {path: sorted(ids) for path, ids in sorted(file_areas.items())}
    return tree, flat, normalized_file_areas


def _resolve_internal_reference(
    reference: str,
    source_path: str,
    paths: set[str],
    suffixes: dict[str, list[str]],
) -> str | None:
    value = reference.replace("\\", "/").strip().removeprefix("./")
    if not value or value.startswith(("http://", "https://")):
        return None
    source_parent = str(PurePosixPath(source_path).parent)
    candidates = [
        value,
        str(PurePosixPath(source_parent) / value),
        str(PurePosixPath("app/src") / value),
    ]
    expanded: list[str] = []
    for candidate in candidates:
        expanded.append(candidate)
        if not PurePosixPath(candidate).suffix:
            expanded.extend(candidate + extension for extension in (".h", ".hpp", ".cpp", ".js", ".jsx", ".ts", ".tsx", ".py"))
    folded_paths = {path.casefold(): path for path in paths}
    for candidate in expanded:
        resolved = folded_paths.get(candidate.casefold())
        if resolved:
            return resolved
    suffix = "/" + value.casefold()
    matches = suffixes.get(suffix, [])
    return matches[0] if len(matches) == 1 else None


def _edge_id(edge_type: str, source: str, target: str) -> str:
    return _stable_id("edge", f"{edge_type}\0{source}\0{target}".casefold())


def _build_graph(
    entries: list[dict[str, Any]],
    units: list[dict[str, Any]],
    modules: list[dict[str, Any]],
    areas: list[dict[str, Any]],
) -> dict[str, Any]:
    paths = {str(entry["path"]) for entry in entries}
    suffixes: dict[str, list[str]] = defaultdict(list)
    for path in paths:
        parts = PurePosixPath(path).parts
        for index in range(len(parts)):
            suffixes["/" + "/".join(parts[index:]).casefold()].append(path)

    nodes: list[dict[str, Any]] = []
    nodes.extend(
        {
            "id": f"file:{entry['path']}",
            "type": "file",
            "label": entry["filename"],
            "path": entry["path"],
            "module": entry["module"],
            "role": entry["architecturalRole"],
            "reviewStatus": entry["reviewStatus"],
            "criticality": entry["criticality"],
        }
        for entry in entries
    )
    nodes.extend(
        {
            "id": unit["id"], "type": "unit", "label": unit["label"],
            "path": unit["primaryPath"], "module": unit["module"], "role": unit["role"],
            "reviewStatus": unit["reviewStatus"], "criticality": unit["criticality"],
        }
        for unit in units
    )
    nodes.extend(
        {
            "id": module["id"], "type": "module", "label": module["label"],
            "path": module["path"], "module": module["path"], "role": "module",
            "reviewStatus": "reviewed", "criticality": "standard",
        }
        for module in modules
    )
    nodes.extend(
        {
            "id": f"area:{area['id']}", "type": "area", "label": area["label"],
            "path": area["id"], "module": "Product Areas", "role": "feature",
            "reviewStatus": "reviewed", "criticality": "standard", "color": area["color"],
        }
        for area in areas
    )

    edges_by_key: dict[tuple[str, str, str], dict[str, Any]] = {}

    def add_edge(edge_type: str, source: str, target: str, confidence: str = "confirmed") -> None:
        if source == target:
            return
        key = (edge_type, source, target)
        edges_by_key[key] = {
            "id": _edge_id(*key), "type": edge_type, "source": source,
            "target": target, "confidence": confidence,
        }

    module_by_path = {module["path"]: module for module in modules}
    unit_by_id = {unit["id"]: unit for unit in units}
    for module in modules:
        parent_path = str(PurePosixPath(module["path"]).parent)
        if parent_path != "." and parent_path in module_by_path:
            add_edge("contains", module_by_path[parent_path]["id"], module["id"])
        for unit_id in module["directUnitIds"]:
            add_edge("contains", module["id"], unit_id)
    for unit in units:
        for path in unit["filePaths"]:
            add_edge("contains", unit["id"], f"file:{path}")
        members = [path for path in unit["filePaths"] if PurePosixPath(path).suffix.casefold() in IMPLEMENTATION_EXTENSIONS]
        headers = [path for path in unit["filePaths"] if PurePosixPath(path).suffix.casefold() in HEADER_EXTENSIONS]
        for implementation in members:
            for header in headers:
                add_edge("implements", f"file:{implementation}", f"file:{header}")
    for area in areas:
        area_node = f"area:{area['id']}"
        if area["parentId"]:
            add_edge("contains", f"area:{area['parentId']}", area_node)
        for unit_id in area["directUnitIds"]:
            if unit_id in unit_by_id:
                add_edge("contains", area_node, unit_id)

    for entry in entries:
        source = f"file:{entry['path']}"
        for related in entry.get("relatedFiles", []):
            if related in paths:
                add_edge("related", source, f"file:{related}")
        for related in entry.get("probableRelatedFiles", []):
            if related in paths and related not in entry.get("relatedFiles", []):
                add_edge("probable", source, f"file:{related}", "probable")
        for field, edge_type in (("includes", "includes"), ("imports", "imports")):
            for reference in entry.get(field, []):
                target = _resolve_internal_reference(reference, entry["path"], paths, suffixes)
                if target:
                    add_edge(edge_type, source, f"file:{target}")

    edge_counts = Counter(edge["type"] for edge in edges_by_key.values())
    return {
        "nodes": sorted(nodes, key=lambda node: (node["type"], str(node["label"]).casefold(), node["id"])),
        "edges": sorted(edges_by_key.values(), key=lambda edge: (edge["type"], edge["source"], edge["target"])),
        "counts": {
            "nodes": len(nodes),
            "edges": len(edges_by_key),
            "edgeTypes": dict(sorted(edge_counts.items())),
        },
    }


def build_explorer_payload(root: Path, entries: list[dict[str, Any]]) -> dict[str, Any]:
    config = load_explorer_config(root)
    errors = validate_explorer_config(config)
    if errors:
        raise ValueError("; ".join(errors))
    decorated_entries = [
        dict(entry, architecturalRole=_role_for(entry, config))
        for entry in sorted(entries, key=lambda item: str(item["path"]).casefold())
    ]
    units, file_to_unit = _build_logical_units(decorated_entries, config)
    architecture_tree, modules = _build_architecture(units)
    product_tree, product_areas, file_areas = _build_product_areas(
        decorated_entries, config, file_to_unit
    )
    for entry in decorated_entries:
        entry["productAreaIds"] = file_areas.get(entry["path"], [])
        entry["logicalUnitId"] = file_to_unit[entry["path"]]
    unmapped_paths = [entry["path"] for entry in decorated_entries if not entry["productAreaIds"]]
    graph = _build_graph(decorated_entries, units, modules, product_areas)
    return {
        "explorerSchemaVersion": config["schemaVersion"],
        "graphSettings": config["graph"],
        "roles": [{"id": role["id"], "label": role["label"], "color": role["color"]} for role in config["roles"]],
        "files": decorated_entries,
        "logicalUnits": units,
        "architectureTree": architecture_tree,
        "modules": modules,
        "productAreaTree": product_tree,
        "productAreas": product_areas,
        "productAreaCoverage": {
            "mappedFiles": len(decorated_entries) - len(unmapped_paths),
            "unmappedFiles": len(unmapped_paths),
            "unmappedPaths": unmapped_paths,
        },
        "graph": graph,
    }
