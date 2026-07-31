#!/usr/bin/env python3
"""Local, rebuildable change history for the Code Registry."""

from __future__ import annotations

import difflib
import json
import sqlite3
import zlib
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable


SCHEMA_VERSION = 1


def _now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def history_path(root: Path, config: dict[str, Any]) -> Path:
    relative = config.get("history", {}).get("database", "Code Registry/data/history.sqlite3")
    return root / str(relative)


def _connect(root: Path, config: dict[str, Any]) -> sqlite3.Connection:
    path = history_path(root, config)
    path.parent.mkdir(parents=True, exist_ok=True)
    connection = sqlite3.connect(path)
    connection.row_factory = sqlite3.Row
    connection.execute("PRAGMA foreign_keys = ON")
    connection.execute("PRAGMA journal_mode = WAL")
    connection.executescript(
        """
        CREATE TABLE IF NOT EXISTS sync_runs (
          id INTEGER PRIMARY KEY,
          created_at TEXT NOT NULL,
          kind TEXT NOT NULL,
          file_count INTEGER NOT NULL,
          summary_json TEXT NOT NULL
        );
        CREATE TABLE IF NOT EXISTS file_versions (
          id INTEGER PRIMARY KEY,
          run_id INTEGER NOT NULL REFERENCES sync_runs(id) ON DELETE CASCADE,
          path TEXT NOT NULL,
          previous_path TEXT,
          event_type TEXT NOT NULL,
          content_hash TEXT NOT NULL,
          line_count INTEGER NOT NULL,
          size_bytes INTEGER NOT NULL,
          content_blob BLOB,
          content_encoding TEXT NOT NULL DEFAULT 'none'
        );
        CREATE INDEX IF NOT EXISTS file_versions_path_run ON file_versions(path, run_id DESC);
        CREATE INDEX IF NOT EXISTS file_versions_hash ON file_versions(content_hash);
        CREATE VIRTUAL TABLE IF NOT EXISTS file_search USING fts5(
          path UNINDEXED, module UNINDEXED, language UNINDEXED, content
        );
        CREATE TABLE IF NOT EXISTS index_state (
          path TEXT PRIMARY KEY,
          content_hash TEXT NOT NULL,
          indexed_at TEXT NOT NULL
        );
        """
    )
    return connection


def _read_text(root: Path, path: str, maximum: int) -> str | None:
    source = root / path
    try:
        payload = source.read_bytes()
    except OSError:
        return None
    if len(payload) > maximum or b"\x00" in payload:
        return None
    return payload.decode("utf-8", errors="replace")


def _events_by_path(events: dict[str, list[Any]]) -> dict[str, tuple[str, str | None]]:
    result: dict[str, tuple[str, str | None]] = {}
    for path in events.get("new", []):
        result[str(path)] = ("created", None)
    for path in events.get("changed", []):
        result[str(path)] = ("modified", None)
    for move in events.get("moved", []):
        result[str(move["to"])] = ("moved", str(move["from"]))
    return result


def _previous_content(connection: sqlite3.Connection, path: str) -> str | None:
    row = connection.execute(
        "SELECT content_blob, content_encoding FROM file_versions WHERE path = ? "
        "AND content_blob IS NOT NULL ORDER BY id DESC LIMIT 1", (path,)
    ).fetchone()
    if row is None:
        return None
    payload = bytes(row["content_blob"])
    if row["content_encoding"] == "zlib-utf8":
        return zlib.decompress(payload).decode("utf-8", errors="replace")
    return None


def _index_entry(connection: sqlite3.Connection, entry: dict[str, Any], content: str) -> None:
    searchable = "\n".join(
        [
            entry["path"], entry.get("module", ""), entry.get("description", ""),
            *entry.get("responsibilities", []), *entry.get("relatedFiles", []),
            *[symbol for values in entry.get("symbols", {}).values() for symbol in values], content[:65536],
        ]
    )
    connection.execute("DELETE FROM file_search WHERE path = ?", (entry["path"],))
    connection.execute(
        "INSERT INTO file_search(path, module, language, content) VALUES (?, ?, ?, ?)",
        (entry["path"], entry.get("module", ""), entry.get("language", ""), searchable),
    )
    connection.execute(
        "INSERT INTO index_state(path, content_hash, indexed_at) VALUES (?, ?, ?) "
        "ON CONFLICT(path) DO UPDATE SET content_hash=excluded.content_hash, indexed_at=excluded.indexed_at",
        (entry["path"], entry["hash"], _now()),
    )


def synchronize_history(root: Path, config: dict[str, Any], entries: Iterable[dict[str, Any]], events: dict[str, list[Any]]) -> dict[str, Any]:
    """Persist one local history run.  A first run stores a baseline, never a fake diff."""
    entries = list(entries)
    maximum = int(config.get("history", {}).get("maxSnapshotBytes", 524288))
    connection = _connect(root, config)
    try:
        baseline = connection.execute("SELECT COUNT(*) FROM sync_runs").fetchone()[0] == 0
        changes = _events_by_path(events)
        if baseline:
            changes = {entry["path"]: ("baseline", None) for entry in entries}
        removed = [str(path) for path in events.get("removed", [])]
        if not changes and not removed:
            return {"created": False, "baseline": False, "changed": 0, "removed": 0}
        with connection:
            summary = {"created": 0, "modified": 0, "moved": 0, "removed": len(removed), "baseline": baseline}
            cursor = connection.execute(
                "INSERT INTO sync_runs(created_at, kind, file_count, summary_json) VALUES (?, ?, ?, ?)",
                (_now(), "baseline" if baseline else "reload", len(changes) + len(removed), json.dumps(summary)),
            )
            run_id = int(cursor.lastrowid)
            by_path = {entry["path"]: entry for entry in entries}
            for path, (event_type, previous_path) in changes.items():
                entry = by_path[path]
                content = _read_text(root, path, maximum)
                blob = zlib.compress(content.encode("utf-8"), level=6) if content is not None else None
                connection.execute(
                    "INSERT INTO file_versions(run_id,path,previous_path,event_type,content_hash,line_count,size_bytes,content_blob,content_encoding) "
                    "VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
                    (run_id, path, previous_path, event_type, entry["hash"], entry.get("lineCount", 0), entry.get("sizeBytes", 0), blob, "zlib-utf8" if blob else "none"),
                )
                if event_type in summary:
                    summary[event_type] += 1
                if content is not None:
                    _index_entry(connection, entry, content)
            for path in removed:
                connection.execute(
                    "INSERT INTO file_versions(run_id,path,previous_path,event_type,content_hash,line_count,size_bytes,content_blob,content_encoding) "
                    "VALUES (?, ?, NULL, 'removed', '', 0, 0, NULL, 'none')", (run_id, path),
                )
                connection.execute("DELETE FROM file_search WHERE path = ?", (path,))
                connection.execute("DELETE FROM index_state WHERE path = ?", (path,))
            connection.execute("UPDATE sync_runs SET summary_json = ? WHERE id = ?", (json.dumps(summary), run_id))
        return {"created": True, "runId": run_id, **summary}
    finally:
        connection.close()


def recent_changes(root: Path, config: dict[str, Any], *, limit: int = 50, path: str | None = None) -> list[dict[str, Any]]:
    connection = _connect(root, config)
    try:
        query = (
            "SELECT fv.path, fv.previous_path, fv.event_type, fv.content_hash, fv.line_count, fv.size_bytes, sr.created_at, sr.id AS run_id "
            "FROM file_versions fv JOIN sync_runs sr ON sr.id = fv.run_id "
        )
        parameters: list[Any] = []
        if path:
            query += "WHERE fv.path = ? OR fv.previous_path = ? "
            parameters.extend([path, path])
        query += "ORDER BY fv.id DESC LIMIT ?"
        parameters.append(max(1, min(limit, 200)))
        return [dict(row) for row in connection.execute(query, parameters)]
    finally:
        connection.close()


def file_diff(root: Path, config: dict[str, Any], path: str, *, limit: int = 800) -> dict[str, Any]:
    connection = _connect(root, config)
    try:
        rows = connection.execute(
            "SELECT fv.*, sr.created_at FROM file_versions fv JOIN sync_runs sr ON sr.id=fv.run_id "
            "WHERE fv.path=? ORDER BY fv.id DESC LIMIT 2", (path,),
        ).fetchall()
        if not rows:
            return {"path": path, "available": False, "reason": "No local history yet."}
        current = rows[0]
        previous = rows[1] if len(rows) > 1 else None
        previous_text = ""
        if previous is not None and previous["content_blob"]:
            previous_text = zlib.decompress(bytes(previous["content_blob"])).decode("utf-8", errors="replace")
        current_text = zlib.decompress(bytes(current["content_blob"])).decode("utf-8", errors="replace") if current["content_blob"] else ""
        diff = list(difflib.unified_diff(previous_text.splitlines(), current_text.splitlines(), fromfile=f"{path} (previous)", tofile=path, lineterm=""))
        clipped = diff[:max(1, min(limit, 5000))]
        return {"path": path, "available": bool(current["content_blob"]), "event": current["event_type"], "createdAt": current["created_at"], "diff": "\n".join(clipped), "truncated": len(diff) > len(clipped)}
    finally:
        connection.close()


def search_local(root: Path, config: dict[str, Any], query: str, *, limit: int = 12) -> list[dict[str, Any]]:
    connection = _connect(root, config)
    try:
        terms = " ".join(part for part in query.replace('"', " ").split() if part)
        if not terms:
            return []
        rows = connection.execute(
            "SELECT path,module,language,snippet(file_search, 3, '[', ']', '…', 14) AS excerpt, bm25(file_search) AS score "
            "FROM file_search WHERE file_search MATCH ? ORDER BY score LIMIT ?", (terms, max(1, min(limit, 50))),
        ).fetchall()
        return [dict(row) for row in rows]
    except sqlite3.OperationalError:
        return []
    finally:
        connection.close()
