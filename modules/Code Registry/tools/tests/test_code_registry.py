#!/usr/bin/env python3
"""Regression tests for Code Registry scan and validation lifecycle contracts."""

from __future__ import annotations

import json
import shutil
import sys
import tempfile
import unittest
from pathlib import Path


TOOLS = Path(__file__).resolve().parents[1]
REGISTRY_ROOT = TOOLS.parent
sys.path.insert(0, str(TOOLS))

from registry_builder import build_outputs, write_modular_registries  # noqa: E402
from scanner import scan_repository  # noqa: E402
from validator import validate_registry  # noqa: E402


class CodeRegistryLifecycleTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        (self.root / "src").mkdir()
        (self.root / "Code Registry" / "registry").mkdir(parents=True)
        (self.root / "Code Registry" / "generated").mkdir(parents=True)
        shutil.copyfile(
            REGISTRY_ROOT / "registry.schema.json",
            self.root / "Code Registry" / "registry.schema.json",
        )
        shutil.copyfile(
            REGISTRY_ROOT / "explorer.json",
            self.root / "Code Registry" / "explorer.json",
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
            "registryFiles": {
                "cpp": "registry/cpp.json",
                "uncategorized": "registry/uncategorized.json",
            },
            "categoryRules": [
                {"category": "cpp", "extensions": [".cpp"]},
                {"category": "uncategorized", "fallback": True},
            ],
            "requireDescription": True,
            "requireModule": True,
            "requireSemanticReviewAfterHashChange": True,
            "maximumResponsibilities": 3,
            "searchDefaultLimit": 20,
        }
        (self.root / "Code Registry" / "config.json").write_text(
            json.dumps(self.config), encoding="utf-8"
        )

    def _source(self, name: str = "Example.cpp", body: str = "int example() { return 1; }\n") -> Path:
        path = self.root / "src" / name
        path.write_text(body, encoding="utf-8")
        return path

    def test_initial_bootstrap_is_reviewed_and_valid(self) -> None:
        self._source()
        entries, events = scan_repository(self.root, self.config)
        build_outputs(self.root, self.config, entries, events)

        self.assertEqual(["src/Example.cpp"], events["new"])
        self.assertEqual("reviewed", entries[0]["reviewStatus"])
        self.assertEqual(entries[0]["hash"], entries[0]["lastReviewedHash"])
        self.assertEqual("approved", validate_registry(self.root, self.config)["status"])

    def test_content_change_preserves_semantics_and_requires_review(self) -> None:
        source = self._source()
        initial, _ = scan_repository(self.root, self.config)
        original_description = initial[0]["description"]
        source.write_text("int example() { return 2; }\n", encoding="utf-8")

        changed, events = scan_repository(self.root, self.config)

        self.assertEqual(["src/Example.cpp"], events["changed"])
        self.assertEqual(original_description, changed[0]["description"])
        self.assertEqual("needs_review", changed[0]["reviewStatus"])
        self.assertNotEqual(changed[0]["hash"], changed[0]["lastReviewedHash"])

    def test_rename_is_detected_by_hash_and_keeps_previous_path(self) -> None:
        source = self._source()
        scan_repository(self.root, self.config)
        source.replace(self.root / "src" / "Renamed.cpp")

        entries, events = scan_repository(self.root, self.config)

        self.assertEqual(
            [{"from": "src/Example.cpp", "to": "src/Renamed.cpp"}], events["moved"]
        )
        self.assertEqual(["src/Example.cpp"], entries[0]["previousPaths"])
        self.assertEqual("needs_review", entries[0]["reviewStatus"])

    def test_validator_reports_unscanned_eligible_file(self) -> None:
        self._source()
        entries, events = scan_repository(self.root, self.config)
        build_outputs(self.root, self.config, entries, events)
        self._source("Untracked.cpp", "int untracked() { return 0; }\n")

        audit = validate_registry(self.root, self.config)

        self.assertEqual("failed", audit["status"])
        self.assertEqual(["src/Untracked.cpp"], audit["issues"]["missing"])

    def test_validator_reports_orphan_without_mutating_registry(self) -> None:
        source = self._source()
        entries, events = scan_repository(self.root, self.config)
        build_outputs(self.root, self.config, entries, events)
        source.unlink()

        audit = validate_registry(self.root, self.config)

        self.assertEqual(["src/Example.cpp"], audit["issues"]["orphaned"])

    def test_validator_reports_duplicate_path(self) -> None:
        self._source()
        entries, events = scan_repository(self.root, self.config)
        duplicated = [entries[0], dict(entries[0])]
        write_modular_registries(self.root, self.config, duplicated)
        build_outputs(self.root, self.config, duplicated, events)

        audit = validate_registry(self.root, self.config)

        self.assertEqual(["Caminho duplicado: src/Example.cpp"], audit["issues"]["duplicates"])

    def test_validator_rejects_empty_semantic_description(self) -> None:
        self._source()
        entries, events = scan_repository(self.root, self.config)
        entries[0]["description"] = ""
        write_modular_registries(self.root, self.config, entries)
        build_outputs(self.root, self.config, entries, events)

        audit = validate_registry(self.root, self.config)

        self.assertIn("src/Example.cpp: descrição vazia", audit["issues"]["classification"])

    def test_validator_rejects_generated_consolidation_conflict(self) -> None:
        self._source()
        entries, events = scan_repository(self.root, self.config)
        build_outputs(self.root, self.config, entries, events)
        path = self.root / "Code Registry" / "generated" / "code-registry.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        payload["entryCount"] = 999
        path.write_text(json.dumps(payload), encoding="utf-8")

        audit = validate_registry(self.root, self.config)

        self.assertIn(
            "code-registry.json: entryCount inconsistente",
            audit["issues"]["generatedConflicts"],
        )

    def test_validator_can_require_complete_product_area_coverage(self) -> None:
        self.config["requireProductAreaCoverage"] = True
        self._source()
        entries, events = scan_repository(self.root, self.config)
        build_outputs(self.root, self.config, entries, events)

        audit = validate_registry(self.root, self.config)

        self.assertIn(
            "src/Example.cpp: arquivo sem Product Area no explorer.json",
            audit["issues"]["classification"],
        )

    def test_validator_marks_unscanned_hash_change_pending(self) -> None:
        source = self._source()
        entries, events = scan_repository(self.root, self.config)
        build_outputs(self.root, self.config, entries, events)
        source.write_text("int example() { return 7; }\n", encoding="utf-8")

        audit = validate_registry(self.root, self.config)

        self.assertEqual(["src/Example.cpp"], audit["issues"]["pending"])
        self.assertTrue(any("hash" in value for value in audit["issues"]["hashes"]))

    def test_removed_file_is_dropped_and_reported_as_scan_event(self) -> None:
        source = self._source()
        scan_repository(self.root, self.config)
        source.unlink()

        entries, events = scan_repository(self.root, self.config)

        self.assertEqual([], entries)
        self.assertEqual(["src/Example.cpp"], events["removed"])


if __name__ == "__main__":
    unittest.main()
