#!/usr/bin/env python3
"""Tests for explainable query expansion and bounded symbol extraction."""

from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch


TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS))

from context_builder import _aliases, read_symbol  # noqa: E402


class ContextBuilderTests(unittest.TestCase):
    def test_expands_domain_terms_with_readable_reasons(self) -> None:
        config = {"retrieval": {"aliases": {"build": ["TimelinePanel"], "grid": ["drawGrid"]}}}

        aliases = _aliases(config, "build grid background")

        self.assertIn(("TimelinePanel", "Alias: build -> TimelinePanel"), aliases)
        self.assertIn(("drawGrid", "Alias: grid -> drawGrid"), aliases)
        self.assertEqual(("build grid background", "Original query"), aliases[0])

    def test_reads_only_requested_symbol_with_line_provenance(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = "src/Example.cpp"
            (root / "src").mkdir()
            (root / path).write_text("void Example::drawGrid() {\n  draw();\n}\nvoid other() {}\n", encoding="utf-8")
            with patch("context_builder._entries", return_value=[{"path": path}]):
                result = read_symbol(root, {}, path, "Example::drawGrid")

        self.assertEqual(1, result["startLine"])
        self.assertEqual(3, result["endLine"])
        self.assertIn("draw();", result["content"])
        self.assertNotIn("void other", result["content"])


if __name__ == "__main__":
    unittest.main()
