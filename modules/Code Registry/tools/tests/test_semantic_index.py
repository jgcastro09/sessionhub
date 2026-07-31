#!/usr/bin/env python3
"""Tests for local semantic-index persistence and ranking."""
from __future__ import annotations

import hashlib
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

TOOLS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS))

from semantic_index import search_semantic, synchronize_semantic  # noqa: E402


class SemanticIndexTests(unittest.TestCase):
    def test_indexes_only_changed_files_and_returns_ranked_matches(self) -> None:
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / "src").mkdir()
            source = "license key input field"
            (root / "src/License.cpp").write_text(source, encoding="utf-8")
            entry = {"path": "src/License.cpp", "hash": hashlib.sha256(source.encode()).hexdigest(), "module": "License", "description": "Draws the license key input.", "responsibilities": ["Accept activation key"], "symbols": {"functions": ["draw"]}}
            config = {"retrieval": {"semantic": {"enabled": True, "database": "Code Registry/data/semantic.sqlite3", "model": "test", "batchSize": 16}}}
            with patch("semantic_index._embed", return_value=[[1.0, 0.0]]) as embed:
                result = synchronize_semantic(root, config, [entry])
                matches = search_semantic(root, config, "key", limit=2)
                second = synchronize_semantic(root, config, [entry])
            self.assertEqual(1, result["indexed"])
            self.assertEqual("src/License.cpp", matches["results"][0]["path"])
            self.assertEqual(0, second["indexed"])
            self.assertEqual(2, embed.call_count)


if __name__ == "__main__":
    unittest.main()
