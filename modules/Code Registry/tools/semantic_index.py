#!/usr/bin/env python3
"""Local Ollama embedding index for intent-aware Code Registry retrieval."""
from __future__ import annotations

import json
import math
import sqlite3
import struct
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Iterable
from urllib.error import URLError
from urllib.request import Request, urlopen


def _now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _settings(config: dict[str, Any]) -> dict[str, Any]:
    return dict(config.get("retrieval", {}).get("semantic", {}))


def _enabled(config: dict[str, Any]) -> bool:
    return bool(_settings(config).get("enabled", False))


def _database(root: Path, config: dict[str, Any]) -> Path:
    return root / str(_settings(config).get("database", "Code Registry/data/semantic.sqlite3"))


def _connect(root: Path, config: dict[str, Any]) -> sqlite3.Connection:
    path = _database(root, config)
    path.parent.mkdir(parents=True, exist_ok=True)
    connection = sqlite3.connect(path)
    connection.row_factory = sqlite3.Row
    connection.execute("PRAGMA journal_mode = WAL")
    connection.execute("CREATE TABLE IF NOT EXISTS semantic_documents (path TEXT PRIMARY KEY, content_hash TEXT NOT NULL, model TEXT NOT NULL, dimensions INTEGER NOT NULL, vector BLOB NOT NULL, indexed_at TEXT NOT NULL)")
    return connection


def _document(root: Path, entry: dict[str, Any], maximum_chars: int) -> str:
    symbols = [symbol for values in entry.get("symbols", {}).values() for symbol in values]
    try:
        source = (root / entry["path"]).read_bytes().decode("utf-8", errors="replace")[:maximum_chars]
    except OSError:
        source = ""
    return "\n".join([f"Path: {entry['path']}", f"Module: {entry.get('module', '')}", f"Description: {entry.get('description', '')}", "Responsibilities: " + "; ".join(entry.get("responsibilities", [])), "Symbols: " + ", ".join(symbols[:100]), "Source:", source])


def _embed(config: dict[str, Any], inputs: list[str]) -> list[list[float]]:
    settings = _settings(config)
    payload = json.dumps({"model": settings.get("model", "qwen3-embedding:0.6b"), "input": inputs}).encode("utf-8")
    request = Request(str(settings.get("endpoint", "http://127.0.0.1:11434/api/embed")), data=payload, headers={"Content-Type": "application/json"})
    try:
        with urlopen(request, timeout=int(settings.get("timeoutSeconds", 120))) as response:
            vectors = json.loads(response.read().decode("utf-8"))["embeddings"]
    except (URLError, OSError, ValueError, KeyError, TimeoutError) as exc:
        raise RuntimeError(f"Ollama embedding unavailable: {exc}") from exc
    if len(vectors) != len(inputs) or not all(isinstance(vector, list) and vector for vector in vectors):
        raise RuntimeError("Ollama returned an invalid embedding response.")
    return [[float(value) for value in vector] for vector in vectors]


def _pack(vector: list[float]) -> bytes:
    return struct.pack(f"<{len(vector)}f", *vector)


def _unpack(payload: bytes, dimensions: int) -> list[float]:
    return list(struct.unpack(f"<{dimensions}f", payload))


def _cosine(left: list[float], right: list[float]) -> float:
    numerator = sum(a * b for a, b in zip(left, right))
    return numerator / (math.sqrt(sum(a * a for a in left)) * math.sqrt(sum(b * b for b in right)))


def synchronize_semantic(root: Path, config: dict[str, Any], entries: Iterable[dict[str, Any]]) -> dict[str, Any]:
    if not _enabled(config):
        return {"enabled": False, "indexed": 0, "skipped": 0}
    entries, settings = list(entries), _settings(config)
    model = str(settings.get("model", "qwen3-embedding:0.6b"))
    connection = _connect(root, config)
    try:
        indexed = {row["path"]: row["content_hash"] for row in connection.execute("SELECT path, content_hash FROM semantic_documents WHERE model = ?", (model,))}
        pending = [entry for entry in entries if indexed.get(entry["path"]) != entry["hash"]]
        with connection:
            connection.executemany("DELETE FROM semantic_documents WHERE path = ?", [(path,) for path in set(indexed) - {entry["path"] for entry in entries}])
        batch_size, max_chars = max(1, min(int(settings.get("batchSize", 16)), 32)), max(1000, min(int(settings.get("documentCharacterLimit", 12000)), 24000))
        for start in range(0, len(pending), batch_size):
            batch = pending[start:start + batch_size]
            vectors = _embed(config, [_document(root, entry, max_chars) for entry in batch])
            with connection:
                for entry, vector in zip(batch, vectors):
                    connection.execute("INSERT INTO semantic_documents(path,content_hash,model,dimensions,vector,indexed_at) VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(path) DO UPDATE SET content_hash=excluded.content_hash, model=excluded.model, dimensions=excluded.dimensions, vector=excluded.vector, indexed_at=excluded.indexed_at", (entry["path"], entry["hash"], model, len(vector), _pack(vector), _now()))
        return {"enabled": True, "indexed": len(pending), "skipped": len(entries) - len(pending), "model": model}
    finally:
        connection.close()


def search_semantic(root: Path, config: dict[str, Any], query: str, *, limit: int = 12) -> dict[str, Any]:
    if not _enabled(config):
        return {"available": False, "reason": "Semantic retrieval is disabled."}
    try:
        query_vector = _embed(config, [query])[0]
        connection = _connect(root, config)
        try:
            rows = connection.execute("SELECT path, dimensions, vector FROM semantic_documents").fetchall()
        finally:
            connection.close()
    except RuntimeError as exc:
        return {"available": False, "reason": str(exc)}
    scored = [(row["path"], _cosine(query_vector, _unpack(bytes(row["vector"]), int(row["dimensions"])))) for row in rows if int(row["dimensions"]) == len(query_vector)]
    scored.sort(key=lambda item: (-item[1], item[0].casefold()))
    return {"available": bool(rows), "model": _settings(config).get("model", "qwen3-embedding:0.6b"), "results": [{"path": path, "score": round(score, 5)} for path, score in scored[:max(1, min(limit, 50))]]}
