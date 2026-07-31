#!/usr/bin/env python3
"""Serve the read-only Code Registry Explorer on localhost with no-cache responses."""

from __future__ import annotations

import json
import mimetypes
import posixpath
import threading
import webbrowser
from functools import partial
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, unquote, urlsplit

from registry_builder import build_outputs
from context_builder import file_context, search_context
from git_audit import audit_git, fetch_remote, file_diff as git_file_diff, file_history as git_file_history, file_source as git_file_source
from history_store import file_diff, recent_changes, synchronize_history
from scanner import load_config, scan_repository
from validator import validate_registry


FALLBACK_RELOAD_LOCK = threading.Lock()


class ExplorerHTTPServer(ThreadingHTTPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, *args: object, **kwargs: object) -> None:
        super().__init__(*args, **kwargs)
        self.reload_lock = threading.Lock()


def refresh_registry(registry_root: Path) -> dict[str, object]:
    """Synchronize registry metadata and return the current audit projection."""
    repository_root = registry_root.resolve().parent
    config = load_config(repository_root)
    entries, events = scan_repository(repository_root, config)
    history = synchronize_history(repository_root, config, entries, events)
    build_outputs(repository_root, config, entries, events)
    audit = validate_registry(repository_root, config, persist_audit=True)
    return {
        "status": audit["status"],
        "counts": audit["counts"],
        "events": events,
        "history": history,
    }


class ExplorerRequestHandler(BaseHTTPRequestHandler):
    server_version = "NodeStageCodeRegistry/1.0"

    def __init__(self, *args: object, registry_root: Path, **kwargs: object) -> None:
        self.registry_root = registry_root.resolve()
        self.web_root = (registry_root / "web").resolve()
        self.generated_root = (registry_root / "generated").resolve()
        super().__init__(*args, **kwargs)

    def _headers(self, content_type: str, length: int, status: HTTPStatus = HTTPStatus.OK) -> None:
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(length))
        self.send_header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
        self.send_header("Pragma", "no-cache")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.send_header("X-Frame-Options", "DENY")
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header(
            "Content-Security-Policy",
            "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "
            "img-src 'self' data:; connect-src 'self'; font-src 'self'; object-src 'none'; frame-ancestors 'none'",
        )
        self.end_headers()

    def _send_bytes(self, data: bytes, content_type: str, *, head_only: bool = False) -> None:
        self._headers(content_type, len(data))
        if not head_only:
            self.wfile.write(data)

    def _send_error(self, status: HTTPStatus, message: str, *, head_only: bool = False) -> None:
        data = (message + "\n").encode("utf-8")
        self._headers("text/plain; charset=utf-8", len(data), status)
        if not head_only:
            self.wfile.write(data)

    def _redirect(self, location: str) -> None:
        self.send_response(HTTPStatus.FOUND)
        self.send_header("Location", location)
        self.send_header("Content-Length", "0")
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()

    @staticmethod
    def _safe_child(root: Path, relative: str) -> Path | None:
        normalized = posixpath.normpath("/" + unquote(relative)).lstrip("/")
        candidate = (root / normalized).resolve()
        try:
            candidate.relative_to(root)
        except ValueError:
            return None
        return candidate

    def _resolve(self, request_path: str) -> Path | None:
        if request_path == "/web/":
            return self.web_root / "index.html"
        if request_path.startswith("/web/"):
            return self._safe_child(self.web_root, request_path.removeprefix("/web/"))
        allowed_generated = {
            "/generated/site-data.json": self.generated_root / "site-data.json",
            "/generated/audit-summary.json": self.generated_root / "audit-summary.json",
        }
        return allowed_generated.get(request_path)

    def _serve(self, *, head_only: bool = False) -> None:
        request = urlsplit(self.path)
        request_path = request.path
        if request_path in {"/", "/web"}:
            self._redirect("/web/")
            return
        if request_path == "/__registry/health":
            site_path = self.generated_root / "site-data.json"
            try:
                payload = json.loads(site_path.read_text(encoding="utf-8"))
                body = json.dumps(
                    {
                        "status": "ok",
                        "generatedAt": payload.get("generatedAt", ""),
                        "files": payload.get("counts", {}).get("total", 0),
                    },
                    ensure_ascii=False,
                ).encode("utf-8")
            except (OSError, UnicodeDecodeError, json.JSONDecodeError):
                self._send_error(HTTPStatus.SERVICE_UNAVAILABLE, "Registry data unavailable", head_only=head_only)
                return
            self._send_bytes(body, "application/json; charset=utf-8", head_only=head_only)
            return
        if request_path == "/__registry/source":
            requested_path = parse_qs(request.query).get("path", [""])[0].replace("\\", "/").removeprefix("./")
            try:
                site_data = json.loads((self.generated_root / "site-data.json").read_text(encoding="utf-8"))
                entry = next((item for item in site_data.get("files", []) if item.get("path") == requested_path), None)
            except (OSError, UnicodeDecodeError, json.JSONDecodeError):
                entry = None
            source_path = self._safe_child(self.registry_root.parent, requested_path)
            if entry is None or source_path is None or not source_path.is_file():
                self._send_error(HTTPStatus.NOT_FOUND, "Registered source file not found", head_only=head_only)
                return
            try:
                payload = source_path.read_bytes()
            except OSError:
                self._send_error(HTTPStatus.INTERNAL_SERVER_ERROR, "Unable to read source file", head_only=head_only)
                return
            if len(payload) > 2 * 1024 * 1024:
                self._send_error(HTTPStatus.REQUEST_ENTITY_TOO_LARGE, "Source file exceeds the 2 MB reader limit", head_only=head_only)
                return
            body = json.dumps(
                {
                    "path": requested_path,
                    "language": entry.get("language", ""),
                    "lastModifiedAt": entry.get("lastModifiedAt", ""),
                    "content": payload.decode("utf-8", errors="replace"),
                },
                ensure_ascii=False,
            ).encode("utf-8")
            self._send_bytes(body, "application/json; charset=utf-8", head_only=head_only)
            return
        if request_path == "/__registry/text-search":
            query = parse_qs(request.query).get("query", [""])[0].strip()
            if not query or len(query) > 200:
                self._send_error(HTTPStatus.BAD_REQUEST, "A search query between 1 and 200 characters is required", head_only=head_only)
                return
            try:
                site_data = json.loads((self.generated_root / "site-data.json").read_text(encoding="utf-8"))
                matches = self._search_source_text(site_data.get("files", []), query)
            except (OSError, UnicodeError, json.JSONDecodeError) as exc:
                self._send_error(HTTPStatus.SERVICE_UNAVAILABLE, f"Source search unavailable: {exc}", head_only=head_only)
                return
            self._send_bytes(json.dumps({"query": query, "files": matches}, ensure_ascii=False).encode("utf-8"), "application/json; charset=utf-8", head_only=head_only)
            return
        if request_path in {"/__registry/git-audit", "/__registry/git-history", "/__registry/git-source", "/__registry/git-diff"}:
            query = parse_qs(request.query)
            requested_path = query.get("path", [""])[0].replace("\\", "/").removeprefix("./")
            try:
                site_data = json.loads((self.generated_root / "site-data.json").read_text(encoding="utf-8"))
                entries = list(site_data.get("files", []))
                registered = {item.get("path") for item in entries}
                if request_path == "/__registry/git-audit":
                    payload = audit_git(self.registry_root.parent, entries)
                elif request_path == "/__registry/git-history":
                    if requested_path not in registered:
                        raise ValueError("Registered path required")
                    payload = git_file_history(self.registry_root.parent, requested_path, limit=int(query.get("limit", ["20"])[0]))
                elif request_path == "/__registry/git-source":
                    if requested_path not in registered:
                        raise ValueError("Registered path required")
                    payload = git_file_source(self.registry_root.parent, requested_path, query.get("ref", ["HEAD"])[0])
                else:
                    if requested_path not in registered:
                        raise ValueError("Registered path required")
                    payload = git_file_diff(self.registry_root.parent, requested_path, ref=query.get("ref", ["HEAD"])[0], working_tree=query.get("mode", [""])[0] == "working")
            except (OSError, UnicodeError, ValueError, RuntimeError, json.JSONDecodeError) as exc:
                self._send_error(HTTPStatus.BAD_REQUEST, str(exc), head_only=head_only)
                return
            self._send_bytes(json.dumps(payload, ensure_ascii=False).encode("utf-8"), "application/json; charset=utf-8", head_only=head_only)
            return
        if request_path in {"/__registry/history", "/__registry/diff", "/__registry/context", "/__registry/search"}:
            query = parse_qs(request.query)
            requested_path = query.get("path", [""])[0].replace("\\", "/").removeprefix("./")
            try:
                site_data = json.loads((self.generated_root / "site-data.json").read_text(encoding="utf-8"))
                registered = {item.get("path") for item in site_data.get("files", [])}
                config = load_config(self.registry_root.parent)
                if request_path == "/__registry/history":
                    if requested_path and requested_path not in registered:
                        raise ValueError("Registered path required")
                    payload = {"changes": recent_changes(self.registry_root.parent, config, path=requested_path or None, limit=int(query.get("limit", ["50"])[0]))}
                elif request_path == "/__registry/diff":
                    if requested_path not in registered:
                        raise ValueError("Registered path required")
                    payload = file_diff(self.registry_root.parent, config, requested_path, limit=int(query.get("limit", ["800"])[0]))
                elif request_path == "/__registry/context":
                    if requested_path not in registered:
                        raise ValueError("Registered path required")
                    payload = file_context(self.registry_root.parent, config, requested_path, budget=int(query.get("budget", ["16000"])[0]), include_source=False)
                else:
                    payload = search_context(self.registry_root.parent, config, query.get("query", [""])[0], limit=int(query.get("limit", ["12"])[0]))
            except (OSError, UnicodeError, ValueError, json.JSONDecodeError) as exc:
                self._send_error(HTTPStatus.BAD_REQUEST, str(exc), head_only=head_only)
                return
            self._send_bytes(json.dumps(payload, ensure_ascii=False).encode("utf-8"), "application/json; charset=utf-8", head_only=head_only)
            return
        path = self._resolve(request_path)
        if path is None or not path.is_file():
            self._send_error(HTTPStatus.NOT_FOUND, "Not found", head_only=head_only)
            return
        try:
            data = path.read_bytes()
        except OSError:
            self._send_error(HTTPStatus.INTERNAL_SERVER_ERROR, "Unable to read resource", head_only=head_only)
            return
        content_type = mimetypes.guess_type(path.name)[0] or "application/octet-stream"
        if content_type.startswith("text/") or content_type in {"application/javascript", "application/json"}:
            content_type += "; charset=utf-8"
        self._send_bytes(data, content_type, head_only=head_only)

    def _search_source_text(self, files: list[dict[str, object]], query: str) -> list[dict[str, object]]:
        """Return bounded literal matches from registered source only, with line provenance."""
        needle = query.casefold()
        results: list[dict[str, object]] = []
        for entry in files:
            path = str(entry.get("path", ""))
            source_path = self._safe_child(self.registry_root.parent, path)
            if source_path is None or not source_path.is_file():
                continue
            try:
                payload = source_path.read_bytes()
            except OSError:
                continue
            if len(payload) > 2 * 1024 * 1024:
                continue
            occurrences = 0
            excerpts: list[dict[str, object]] = []
            for line_number, line in enumerate(payload.decode("utf-8", errors="replace").splitlines(), start=1):
                count = line.casefold().count(needle)
                if not count:
                    continue
                occurrences += count
                if len(excerpts) < 5:
                    excerpts.append({"line": line_number, "text": line.strip()[:500]})
            if occurrences:
                results.append({"path": path, "language": entry.get("language", ""), "occurrences": occurrences, "matches": excerpts})
                if len(results) >= 100:
                    break
        return results

    def _reload(self) -> None:
        reload_lock = getattr(self.server, "reload_lock", FALLBACK_RELOAD_LOCK)
        with reload_lock:
            try:
                payload = refresh_registry(self.registry_root)
                data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
            except (OSError, UnicodeError, json.JSONDecodeError, RuntimeError, ValueError) as exc:
                self._send_error(HTTPStatus.INTERNAL_SERVER_ERROR, f"Registry reload failed: {exc}")
                return
        self._send_bytes(data, "application/json; charset=utf-8")

    def do_GET(self) -> None:  # noqa: N802 - HTTP handler API
        self._serve()

    def do_HEAD(self) -> None:  # noqa: N802 - HTTP handler API
        self._serve(head_only=True)

    def do_POST(self) -> None:  # noqa: N802 - HTTP handler API
        request_path = urlsplit(self.path).path
        if request_path == "/__registry/git-fetch":
            try:
                site_data = json.loads((self.generated_root / "site-data.json").read_text(encoding="utf-8"))
                payload = fetch_remote(self.registry_root.parent, site_data.get("files", []))
            except (OSError, UnicodeError, ValueError, RuntimeError, json.JSONDecodeError) as exc:
                self._send_error(HTTPStatus.BAD_REQUEST, str(exc))
                return
            self._send_bytes(json.dumps(payload, ensure_ascii=False).encode("utf-8"), "application/json; charset=utf-8")
            return
        if request_path != "/__registry/reload":
            self._send_error(HTTPStatus.NOT_FOUND, "Not found")
            return
        self._reload()

    def log_message(self, format_text: str, *args: object) -> None:
        return


def serve_explorer(registry_root: Path, port: int, *, open_browser: bool = True) -> int:
    if not 1 <= port <= 65535:
        raise ValueError("A porta deve estar entre 1 e 65535.")
    site_data = registry_root / "generated" / "site-data.json"
    index = registry_root / "web" / "index.html"
    if not site_data.is_file() or not index.is_file():
        raise RuntimeError("Explorer ou site-data.json ausente; execute o comando full primeiro.")
    handler = partial(ExplorerRequestHandler, registry_root=registry_root)
    try:
        server = ExplorerHTTPServer(("127.0.0.1", port), handler)
    except OSError as exc:
        raise RuntimeError(f"Não foi possível abrir 127.0.0.1:{port}: {exc}") from exc
    url = f"http://127.0.0.1:{port}/web/"
    print(f"Code Registry Explorer: {url}")
    print("Servidor local; Reload data sincroniza somente os metadados do Code Registry. Pressione Ctrl+C para encerrar.")
    if open_browser:
        webbrowser.open(url)
    try:
        server.serve_forever(poll_interval=0.25)
    except KeyboardInterrupt:
        print("\nCode Registry Explorer: encerrado.")
    finally:
        server.server_close()
    return 0
