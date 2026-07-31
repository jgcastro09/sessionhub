#!/usr/bin/env python3
"""Read-only Git state, history, and diff helpers for the Code Registry."""

from __future__ import annotations

import re
import subprocess
from pathlib import Path
from typing import Any, Iterable


_REF = re.compile(r"^(HEAD|[0-9a-fA-F]{7,64})$")


def _git(root: Path, arguments: list[str], *, allow_failure: bool = False) -> str:
    process = subprocess.run(
        ["git", "-C", str(root), *arguments], capture_output=True, text=True,
        encoding="utf-8", errors="replace", check=False,
    )
    if process.returncode and not allow_failure:
        raise RuntimeError(process.stderr.strip() or "Git command failed.")
    return process.stdout


def _field_lines(payload: str) -> list[list[str]]:
    return [line.split("\x1f") for line in payload.splitlines() if line]


def _changes(root: Path) -> list[dict[str, str]]:
    records = _git(root, ["status", "--porcelain=v1", "-z", "--untracked-files=all"]).split("\0")
    changes: list[dict[str, str]] = []
    index = 0
    while index < len(records):
        record = records[index]
        index += 1
        if not record:
            continue
        status, path = record[:2], record[3:]
        previous_path = ""
        if status[0] in {"R", "C"} and index < len(records):
            previous_path = records[index]
            index += 1
        changes.append({
            "status": status,
            "path": path.replace("\\", "/"),
            "previousPath": previous_path.replace("\\", "/"),
            "staged": str(status[0] not in {" ", "?"}).lower(),
            "worktree": str(status[1] not in {" ", "?"}).lower(),
            "untracked": str(status == "??").lower(),
            "conflicted": str("U" in status or status in {"AA", "DD"}).lower(),
        })
    return changes


def _head(root: Path) -> dict[str, str]:
    fields = _field_lines(_git(root, ["log", "-1", "--format=%H%x1f%h%x1f%an%x1f%aI%x1f%s"]))
    if not fields:
        return {}
    values = fields[0]
    return dict(zip(("hash", "shortHash", "author", "date", "subject"), values, strict=False))


def _commits(root: Path, path: str | None = None, limit: int = 15) -> list[dict[str, str]]:
    arguments = ["log", f"-n{max(1, min(limit, 50))}", "--format=%H%x1f%h%x1f%an%x1f%aI%x1f%s"]
    if path:
        arguments.extend(["--", path])
    keys = ("hash", "shortHash", "author", "date", "subject")
    return [dict(zip(keys, values, strict=False)) for values in _field_lines(_git(root, arguments))]


def audit_git(root: Path, entries: Iterable[dict[str, Any]] = ()) -> dict[str, Any]:
    """Summarize local Git health without changing worktree or remote refs."""
    if _git(root, ["rev-parse", "--is-inside-work-tree"], allow_failure=True).strip() != "true":
        return {"available": False, "reason": "This folder is not a Git worktree."}
    branch = _git(root, ["symbolic-ref", "--quiet", "--short", "HEAD"], allow_failure=True).strip() or "detached"
    upstream = _git(root, ["rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"], allow_failure=True).strip()
    ahead = behind = 0
    if upstream:
        counts = _git(root, ["rev-list", "--left-right", "--count", f"HEAD...{upstream}"]).split()
        if len(counts) == 2:
            ahead, behind = (int(value) for value in counts)
    changes = _changes(root)
    registered = {str(entry.get("path", "")) for entry in entries}
    pending = [str(entry["path"]) for entry in entries if entry.get("reviewStatus") != "reviewed"]
    registered_changes = [change for change in changes if change["path"] in registered or change["previousPath"] in registered]
    return {
        "available": True,
        "branch": branch,
        "upstream": upstream,
        "remote": _git(root, ["remote", "get-url", "origin"], allow_failure=True).strip(),
        "head": _head(root),
        "ahead": ahead,
        "behind": behind,
        "changes": changes,
        "conflicts": [change for change in changes if change["conflicted"] == "true"],
        "recentCommits": _commits(root),
        "registry": {
            "changedRegisteredFiles": registered_changes,
            "pendingSemanticReview": pending,
            "healthy": not pending,
        },
    }


def fetch_remote(root: Path, entries: Iterable[dict[str, Any]] = ()) -> dict[str, Any]:
    """Update remote refs only; never merge, pull, alter source, or resolve conflicts."""
    _git(root, ["fetch", "--prune", "origin"])
    return audit_git(root, entries)


def _checked_ref(ref: str) -> str:
    if not _REF.fullmatch(ref):
        raise ValueError("Only HEAD or a commit hash can be read.")
    return ref


def file_history(root: Path, path: str, *, limit: int = 20) -> dict[str, Any]:
    return {"path": path, "commits": _commits(root, path, limit=limit)}


def file_source(root: Path, path: str, ref: str) -> dict[str, Any]:
    checked_ref = _checked_ref(ref)
    content = _git(root, ["show", f"{checked_ref}:{path}"])
    if len(content.encode("utf-8")) > 2 * 1024 * 1024:
        raise ValueError("Git source exceeds the 2 MB reader limit.")
    return {"path": path, "ref": checked_ref, "content": content}


def file_diff(root: Path, path: str, *, ref: str = "HEAD", working_tree: bool = False) -> dict[str, Any]:
    if working_tree:
        diff = _git(root, ["diff", "--no-ext-diff", "--unified=3", "HEAD", "--", path])
        return {"path": path, "base": "HEAD", "target": "working tree", "diff": diff, "available": bool(diff)}
    checked_ref = _checked_ref(ref)
    parents = _git(root, ["rev-list", "--parents", "-n", "1", checked_ref]).split()
    if len(parents) < 2:
        return {"path": path, "base": "", "target": checked_ref, "diff": "", "available": False, "reason": "The selected commit has no parent."}
    base = parents[1]
    diff = _git(root, ["diff", "--no-ext-diff", "--unified=3", base, checked_ref, "--", path])
    return {"path": path, "base": base, "target": checked_ref, "diff": diff, "available": bool(diff)}
