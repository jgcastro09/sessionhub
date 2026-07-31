#!/usr/bin/env python3
"""Regression tests for Explorer projections and its restricted localhost server."""

from __future__ import annotations

import copy
import hashlib
import json
import shutil
import sys
import tempfile
import threading
import unittest
from functools import partial
from http.server import ThreadingHTTPServer
from pathlib import Path
from urllib.error import HTTPError
from urllib.request import urlopen


TOOLS = Path(__file__).resolve().parents[1]
REGISTRY_ROOT = TOOLS.parent
sys.path.insert(0, str(TOOLS))

from explorer_builder import build_explorer_payload, load_explorer_config, validate_explorer_config  # noqa: E402
from explorer_server import ExplorerRequestHandler  # noqa: E402
from registry_builder import build_outputs  # noqa: E402
from scanner import scan_repository  # noqa: E402


def entry(path: str, *, includes: list[str] | None = None) -> dict[str, object]:
    source = Path(path)
    return {
        "path": path,
        "filename": source.name,
        "extension": source.suffix,
        "language": "C++",
        "kind": "header" if source.suffix == ".h" else "implementation",
        "category": "cpp",
        "module": "Application",
        "description": f"Implements {source.stem} workspace coordination.",
        "responsibilities": ["Coordinate workspace state"],
        "criticality": "standard",
        "relatedFiles": [],
        "probableRelatedFiles": [],
        "previousPaths": [],
        "dependencies": [],
        "includes": includes or [],
        "imports": [],
        "exports": [],
        "symbols": {"classes": [source.stem], "functions": []},
        "lineCount": 12,
        "sizeBytes": 240,
        "lastModifiedAt": "2026-07-22T12:00:00Z",
        "hash": "a" * 64,
        "lastReviewedHash": "a" * 64,
        "reviewStatus": "reviewed",
        "status": "active",
        "lastScannedAt": "2026-07-22T12:00:00Z",
    }


class ExplorerProjectionTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "Code Registry").mkdir()
        shutil.copyfile(REGISTRY_ROOT / "explorer.json", self.root / "Code Registry" / "explorer.json")

    def test_header_and_implementation_form_one_logical_unit(self) -> None:
        entries = [
            entry("app/src/app/AppWorkspaceService.cpp", includes=["app/AppWorkspaceService.h"]),
            entry("app/src/app/AppWorkspaceService.h"),
        ]

        payload = build_explorer_payload(self.root, entries)

        self.assertEqual(1, len(payload["logicalUnits"]))
        unit = payload["logicalUnits"][0]
        self.assertEqual(2, len(unit["filePaths"]))
        workspaces = next(area for area in payload["productAreas"] if area["id"] == "workspaces")
        self.assertEqual(2, workspaces["counts"]["files"])
        self.assertEqual({"mappedFiles": 2, "unmappedFiles": 0, "unmappedPaths": []}, payload["productAreaCoverage"])
        edge_types = payload["graph"]["counts"]["edgeTypes"]
        self.assertEqual(1, edge_types["implements"])
        self.assertEqual(1, edge_types["includes"])

    def test_projection_is_deterministic_for_reordered_entries(self) -> None:
        entries = [entry("app/src/app/AppWorkspaceService.cpp"), entry("app/src/app/AppWorkspaceService.h")]

        first = build_explorer_payload(self.root, entries)
        second = build_explorer_payload(self.root, list(reversed(entries)))

        self.assertEqual(first, second)

    def test_duplicate_product_area_id_is_rejected(self) -> None:
        config = copy.deepcopy(load_explorer_config(self.root))
        config["productAreas"].append(copy.deepcopy(config["productAreas"][0]))

        errors = validate_explorer_config(config)

        self.assertTrue(any("id duplicado 'workspaces'" in error for error in errors))

    def test_vendored_cytoscape_hash_is_pinned(self) -> None:
        vendor = REGISTRY_ROOT / "web" / "vendor" / "cytoscape.min.js"

        digest = hashlib.sha256(vendor.read_bytes()).hexdigest()

        self.assertEqual("9c2a3bf2592e0b14a1f7bec07c03a54f16dedf32af9cd0af155c716aa6c87bc3", digest)


class ExplorerServerTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.registry_root = Path(self.temporary.name) / "Code Registry"
        self.repository_root = self.registry_root.parent
        (self.registry_root / "web").mkdir(parents=True)
        (self.registry_root / "generated").mkdir()
        (self.registry_root / "registry").mkdir()
        (self.repository_root / "src").mkdir()
        shutil.copyfile(
            REGISTRY_ROOT / "registry.schema.json",
            self.registry_root / "registry.schema.json",
        )
        shutil.copyfile(
            REGISTRY_ROOT / "explorer.json",
            self.registry_root / "explorer.json",
        )
        self.config = {
            "schemaVersion": 1,
            "scanRoots": ["src"],
            "extensions": [".cpp"],
            "exactFilenames": [],
            "explicitIncludeFiles": [],
            "explicitIncludeReasons": {},
            "excludeDirectoryNames": {},
            "excludePathPrefixes": {},
            "excludeFiles": {},
            "registryFiles": {"cpp": "registry/cpp.json", "uncategorized": "registry/uncategorized.json"},
            "categoryRules": [{"category": "cpp", "extensions": [".cpp"]}, {"category": "uncategorized", "fallback": True}],
            "requireDescription": True,
            "requireModule": True,
            "requireSemanticReviewAfterHashChange": True,
            "maximumResponsibilities": 3,
            "searchDefaultLimit": 20,
        }
        (self.registry_root / "config.json").write_text(json.dumps(self.config), encoding="utf-8")
        (self.repository_root / "src" / "Example.cpp").write_text("int example() { return 1; }\n", encoding="utf-8")
        entries, events = scan_repository(self.repository_root, self.config)
        build_outputs(self.repository_root, self.config, entries, events)
        (self.registry_root / "web" / "index.html").write_text("<!doctype html><title>Explorer</title>", encoding="utf-8")
        handler = partial(ExplorerRequestHandler, registry_root=self.registry_root)
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.addCleanup(self._stop_server)
        self.base_url = f"http://127.0.0.1:{self.server.server_port}"

    def _stop_server(self) -> None:
        self.server.shutdown()
        self.server.server_close()
        self.thread.join(timeout=2)

    def test_serves_explorer_with_no_cache_and_security_headers(self) -> None:
        with urlopen(f"{self.base_url}/", timeout=3) as response:
            body = response.read().decode("utf-8")
            self.assertIn("Explorer", body)
            self.assertTrue(response.geturl().endswith("/web/"))
            self.assertIn("no-store", response.headers["Cache-Control"])
            self.assertIn("default-src 'self'", response.headers["Content-Security-Policy"])

    def test_does_not_expose_modular_registry(self) -> None:
        try:
            urlopen(f"{self.base_url}/registry/cpp.json", timeout=3)
        except HTTPError as exc:
            self.assertEqual(404, exc.code)
            exc.close()
        else:
            self.fail("The restricted server exposed a modular registry path.")

    def test_serves_only_registered_source_files_to_code_reader(self) -> None:
        with urlopen(f"{self.base_url}/__registry/source?path=src%2FExample.cpp", timeout=3) as response:
            payload = json.loads(response.read().decode("utf-8"))

        self.assertEqual("src/Example.cpp", payload["path"])
        self.assertIn("int example()", payload["content"])
        try:
            urlopen(f"{self.base_url}/__registry/source?path=..%2Foutside.cpp", timeout=3)
        except HTTPError as exc:
            self.assertEqual(404, exc.code)
            exc.close()
        else:
            self.fail("The code reader exposed a path outside the registered inventory.")

    def test_searches_registered_source_text_with_line_provenance(self) -> None:
        with urlopen(f"{self.base_url}/__registry/text-search?query=example", timeout=3) as response:
            payload = json.loads(response.read().decode("utf-8"))

        self.assertEqual("src/Example.cpp", payload["files"][0]["path"])
        self.assertEqual(1, payload["files"][0]["matches"][0]["line"])
        self.assertIn("example", payload["files"][0]["matches"][0]["text"])

    def test_reload_endpoint_discovers_new_source_file(self) -> None:
        (self.repository_root / "src" / "NewFile.cpp").write_text("int new_file() { return 2; }\n", encoding="utf-8")
        request = __import__("urllib.request", fromlist=["Request"]).Request(
            f"{self.base_url}/__registry/reload", data=b"", method="POST"
        )

        try:
            with urlopen(request, timeout=3) as response:
                payload = json.loads(response.read().decode("utf-8"))
        except HTTPError as exc:
            self.fail(exc.read().decode("utf-8"))

        self.assertEqual("failed", payload["status"])
        self.assertEqual(["src/NewFile.cpp"], payload["events"]["new"])
        self.assertEqual(1, payload["counts"]["pending"])
        site_data = json.loads((self.registry_root / "generated" / "site-data.json").read_text(encoding="utf-8"))
        self.assertEqual(2, site_data["counts"]["total"])
        (self.repository_root / "src" / "NewFile.cpp").unlink()

        with urlopen(request, timeout=3) as response:
            payload = json.loads(response.read().decode("utf-8"))

        self.assertEqual(["src/NewFile.cpp"], payload["events"]["removed"])
        self.assertEqual("approved", payload["status"])
        site_data = json.loads((self.registry_root / "generated" / "site-data.json").read_text(encoding="utf-8"))
        self.assertEqual(1, site_data["counts"]["total"])


if __name__ == "__main__":
    unittest.main()
