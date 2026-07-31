#!/usr/bin/env python3
"""Tests for local Registry history, diff generation, and FTS retrieval."""

from __future__ import annotations

import hashlib
import sys
import tempfile
import unittest
from pathlib import Path


TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS))

from history_store import file_diff, recent_changes, search_local, synchronize_history  # noqa: E402


def entry(path: str, content: str) -> dict[str, object]:
    source = Path(path)
    return {
        "path": path, "module": "Testing", "description": "Implements local history test coverage.",
        "responsibilities": ["Verify local history"], "relatedFiles": [], "symbols": {"functions": ["example"]},
        "language": "C++", "hash": hashlib.sha256(content.encode("utf-8")).hexdigest(),
        "lineCount": content.count("\n"), "sizeBytes": len(content.encode("utf-8")),
    }


class HistoryStoreTests(unittest.TestCase):
    def setUp(self) -> None:
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.path = "src/Example.cpp"
        (self.root / "src").mkdir()
        self.config = {"history": {"database": "Code Registry/data/history.sqlite3", "maxSnapshotBytes": 4096}}

    def test_baseline_modification_diff_and_search(self) -> None:
        first = "int example() { return 1; }\n"
        (self.root / self.path).write_text(first, encoding="utf-8")
        synchronize_history(self.root, self.config, [entry(self.path, first)], {"new": [], "changed": [], "moved": [], "removed": []})
        self.assertEqual("baseline", recent_changes(self.root, self.config, path=self.path)[0]["event_type"])
        self.assertEqual(self.path, search_local(self.root, self.config, "example")[0]["path"])

        second = "int example() { return 2; }\n"
        (self.root / self.path).write_text(second, encoding="utf-8")
        synchronize_history(self.root, self.config, [entry(self.path, second)], {"new": [], "changed": [self.path], "moved": [], "removed": []})
        diff = file_diff(self.root, self.config, self.path)
        self.assertIn("return 1", diff["diff"])
        self.assertIn("return 2", diff["diff"])
        self.assertEqual("modified", recent_changes(self.root, self.config, path=self.path)[0]["event_type"])

    def test_removed_path_is_not_searchable(self) -> None:
        source = "int example() { return 1; }\n"
        (self.root / self.path).write_text(source, encoding="utf-8")
        synchronize_history(self.root, self.config, [entry(self.path, source)], {"new": [], "changed": [], "moved": [], "removed": []})
        synchronize_history(self.root, self.config, [], {"new": [], "changed": [], "moved": [], "removed": [self.path]})
        self.assertEqual([], search_local(self.root, self.config, "example"))
        self.assertEqual("removed", recent_changes(self.root, self.config, path=self.path)[0]["event_type"])


if __name__ == "__main__":
    unittest.main()
