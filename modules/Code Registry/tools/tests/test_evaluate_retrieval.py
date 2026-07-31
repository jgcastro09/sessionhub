#!/usr/bin/env python3
"""Tests for deterministic retrieval-evaluation metrics."""

from __future__ import annotations

import sys
import unittest


TOOLS = __import__("pathlib").Path(__file__).resolve().parents[1]
sys.path.insert(0, str(TOOLS))

from evaluate_retrieval import _rank  # noqa: E402


class RetrievalEvaluationTests(unittest.TestCase):
    def test_rank_is_one_based_and_reports_missing(self) -> None:
        self.assertEqual(2, _rank(["first.cpp", "target.cpp"], "target.cpp"))
        self.assertIsNone(_rank(["first.cpp"], "missing.cpp"))


if __name__ == "__main__":
    unittest.main()
