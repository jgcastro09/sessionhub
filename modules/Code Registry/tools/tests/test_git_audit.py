#!/usr/bin/env python3
"""Tests for read-only Git audit, history, source, and diff helpers."""
from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS))

from git_audit import audit_git, file_diff, file_history, file_source  # noqa: E402


def git(root: Path, *arguments: str) -> None:
    subprocess.run(["git", "-C", str(root), *arguments], check=True, capture_output=True, text=True)


class GitAuditTests(unittest.TestCase):
    def test_reports_dirty_registered_file_and_reads_committed_history(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "src" / "Example.cpp"
            source.parent.mkdir()
            source.write_text("int example() { return 1; }\n", encoding="utf-8")
            git(root, "init", "-b", "main")
            git(root, "config", "user.name", "Test")
            git(root, "config", "user.email", "test@example.com")
            git(root, "add", "src/Example.cpp")
            git(root, "commit", "-m", "Initial source")
            source.write_text("int example() { return 2; }\n", encoding="utf-8")

            audit = audit_git(root, [{"path": "src/Example.cpp", "reviewStatus": "reviewed"}])
            history = file_history(root, "src/Example.cpp")
            committed = file_source(root, "src/Example.cpp", "HEAD")
            diff = file_diff(root, "src/Example.cpp", working_tree=True)

        self.assertTrue(audit["available"])
        self.assertEqual("main", audit["branch"])
        self.assertEqual("src/Example.cpp", audit["registry"]["changedRegisteredFiles"][0]["path"])
        self.assertEqual(1, len(history["commits"]))
        self.assertIn("return 1", committed["content"])
        self.assertTrue(diff["available"])
        self.assertIn("return 2", diff["diff"])


if __name__ == "__main__":
    unittest.main()
