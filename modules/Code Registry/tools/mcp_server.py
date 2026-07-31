#!/usr/bin/env python3
"""Read-only stdio MCP server for the local Code Registry."""

from __future__ import annotations

import json
import sys
from pathlib import Path
from typing import Any

from context_builder import file_context, locate_ui_surface, module_context, read_symbol, search_context
from git_audit import audit_git, file_diff as git_file_diff, file_history as git_file_history
from history_store import file_diff, recent_changes
from registry_builder import load_registry_entries


ROOT = Path(__file__).resolve().parents[2]


def _configure_stdio() -> None:
    """MCP transports are UTF-8 even when Windows inherited a legacy console code page."""
    for stream in (sys.stdin, sys.stdout, sys.stderr):
        reconfigure = getattr(stream, "reconfigure", None)
        if reconfigure is not None:
            reconfigure(encoding="utf-8", errors="strict")


TOOLS = [
    {"name": "search_code", "description": "Use for broad or ambiguous natural-language requests where direct text search lacks a known label, symbol, path, or identifier. Searches registered code with domain aliases and explains the ranking.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {"query": {"type": "string"}, "limit": {"type": "integer", "minimum": 1, "maximum": 50}}, "required": ["query"]}},
    {"name": "locate_ui_surface", "description": "Use for an ambiguous visual surface such as an unnamed canvas, grid, panel, color, or layout. Prefer direct text search when the visible label is known. Traces the concept to workspace owner, renderer, and theme consumers.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {"surface": {"type": "string"}, "limit": {"type": "integer", "minimum": 1, "maximum": 20}}, "required": ["surface"]}},
    {"name": "read_symbol", "description": "Use after locating a relevant file to read only one bounded function or method body, with source line provenance.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}, "symbol": {"type": "string"}, "maximumChars": {"type": "integer", "minimum": 1000, "maximum": 60000}}, "required": ["path", "symbol"]}},
    {"name": "get_file_context", "description": "Return a bounded context pack for one already-identified registered file.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}, "budget": {"type": "integer"}, "includeSource": {"type": "boolean"}}, "required": ["path"]}},
    {"name": "get_module_context", "description": "Return a bounded architecture context pack for an already-identified module.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {"module": {"type": "string"}, "budget": {"type": "integer"}}, "required": ["module"]}},
    {"name": "get_recent_changes", "description": "List local history events captured by Reload data.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}, "limit": {"type": "integer", "minimum": 1, "maximum": 200}}}},
    {"name": "get_file_diff", "description": "Return the latest local diff for a registered file.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}, "limit": {"type": "integer", "minimum": 1, "maximum": 5000}}, "required": ["path"]}},
    {"name": "get_git_audit", "description": "Read the Git branch, upstream divergence, working-tree changes, conflicts, recent commits, and correlation with registered files. It never fetches, pulls, commits, pushes, or changes source files.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {}}},
    {"name": "get_git_file_history", "description": "List Git commits that touched one registered file.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}, "limit": {"type": "integer", "minimum": 1, "maximum": 50}}, "required": ["path"]}},
    {"name": "get_git_file_diff", "description": "Return the Git diff for a registered file: working tree versus HEAD, or one commit versus its parent.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {"path": {"type": "string"}, "ref": {"type": "string"}, "workingTree": {"type": "boolean"}}, "required": ["path"]}},
    {"name": "registry_health", "description": "Return Registry coverage and local-history health without source content.", "annotations": {"readOnlyHint": True}, "inputSchema": {"type": "object", "properties": {}}},
]


def _config() -> dict[str, Any]:
    return json.loads((ROOT / "Code Registry" / "config.json").read_text(encoding="utf-8"))


def _registered(config: dict[str, Any], path: str) -> str:
    normalized = path.replace("\\", "/").removeprefix("./")
    entries, errors = load_registry_entries(ROOT, config)
    if errors:
        raise RuntimeError("; ".join(errors))
    if normalized not in {entry["path"] for entry in entries}:
        raise ValueError("Path is not registered and cannot be read through MCP.")
    return normalized


def _result(value: Any) -> dict[str, Any]:
    return {"content": [{"type": "text", "text": json.dumps(value, ensure_ascii=False, indent=2)}]}


def _call(name: str, arguments: dict[str, Any]) -> dict[str, Any]:
    config = _config()
    if name == "search_code":
        return _result(search_context(ROOT, config, str(arguments.get("query", "")), limit=int(arguments.get("limit", 12))))
    if name == "locate_ui_surface":
        return _result(locate_ui_surface(ROOT, config, str(arguments["surface"]), limit=int(arguments.get("limit", 8))))
    if name == "read_symbol":
        path = _registered(config, str(arguments["path"]))
        return _result(read_symbol(ROOT, config, path, str(arguments["symbol"]), maximum_chars=int(arguments.get("maximumChars", 16000))))
    if name == "get_file_context":
        path = _registered(config, str(arguments["path"]))
        return _result(file_context(ROOT, config, path, budget=arguments.get("budget"), include_source=bool(arguments.get("includeSource", True))))
    if name == "get_module_context":
        return _result(module_context(ROOT, config, str(arguments["module"]), budget=arguments.get("budget")))
    if name == "get_recent_changes":
        path = arguments.get("path")
        if path is not None:
            path = _registered(config, str(path))
        return _result({"changes": recent_changes(ROOT, config, path=path, limit=int(arguments.get("limit", 50)))})
    if name == "get_file_diff":
        path = _registered(config, str(arguments["path"]))
        return _result(file_diff(ROOT, config, path, limit=int(arguments.get("limit", 800))))
    if name == "get_git_audit":
        entries, errors = load_registry_entries(ROOT, config)
        if errors:
            raise RuntimeError("; ".join(errors))
        return _result(audit_git(ROOT, entries))
    if name == "get_git_file_history":
        path = _registered(config, str(arguments["path"]))
        return _result(git_file_history(ROOT, path, limit=int(arguments.get("limit", 20))))
    if name == "get_git_file_diff":
        path = _registered(config, str(arguments["path"]))
        return _result(git_file_diff(ROOT, path, ref=str(arguments.get("ref", "HEAD")), working_tree=bool(arguments.get("workingTree", False))))
    if name == "registry_health":
        audit_path = ROOT / "Code Registry" / "generated" / "audit-summary.json"
        return _result({"audit": json.loads(audit_path.read_text(encoding="utf-8")), "mode": "local read-only stdio"})
    raise ValueError("Unknown MCP tool.")


def _response(request_id: Any, result: Any = None, error: str | None = None) -> dict[str, Any]:
    payload: dict[str, Any] = {"jsonrpc": "2.0", "id": request_id}
    if error is None:
        payload["result"] = result
    else:
        payload["error"] = {"code": -32602, "message": error}
    return payload


def serve() -> int:
    _configure_stdio()
    for line in sys.stdin:
        try:
            request = json.loads(line)
            method = request.get("method")
            request_id = request.get("id")
            if method == "initialize":
                result = {"protocolVersion": request.get("params", {}).get("protocolVersion", "2024-11-05"), "capabilities": {"tools": {}}, "instructions": "Use these tools for broad, ambiguous, cross-module, or concept-only code requests. If the request includes a known label, symbol, path, or exact identifier, prefer direct search and source reading. For ambiguous visual surfaces use locate_ui_surface; for unclear ownership use search_code; then use read_symbol or get_file_context for the smallest relevant source. Do not require users to supply tool names. All tools are read-only.", "serverInfo": {"name": "nodestage-code-registry", "version": "1.2.0"}}
            elif method == "tools/list":
                result = {"tools": TOOLS}
            elif method == "tools/call":
                params = request.get("params", {})
                result = _call(str(params.get("name", "")), dict(params.get("arguments", {})))
            elif method == "notifications/initialized":
                continue
            else:
                raise ValueError("Unsupported MCP method.")
            if request_id is not None:
                print(json.dumps(_response(request_id, result), ensure_ascii=False), flush=True)
        except (KeyError, TypeError, ValueError, OSError, UnicodeError, json.JSONDecodeError, RuntimeError) as exc:
            if 'request_id' in locals() and request_id is not None:
                print(json.dumps(_response(request_id, error=str(exc)), ensure_ascii=False), flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(serve())
