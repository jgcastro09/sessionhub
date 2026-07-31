"""Deterministic Task Board evidence audits over the local repository."""

from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path


SCRIPT_DIR = Path(__file__).resolve().parent
REPOSITORY_ROOT = SCRIPT_DIR.parents[1]
CODE_REGISTRY_TOOL = REPOSITORY_ROOT / "Code Registry" / "tools" / "code_registry.py"

VALIDATION_COMMANDS = {
    "code-registry-validate": [sys.executable, str(CODE_REGISTRY_TOOL), "validate"],
    "change-contract": [sys.executable, "tools/agent/validate_change_contract.py"],
    "task-board-markdown-storage": [sys.executable, "task-board/tests/task_board_markdown_storage.test.py"],
    "task-board-card-importer": ["node", "task-board/tests/card-importer.test.js"],
}


def _repository_file(value: str) -> Path | None:
    candidate = (REPOSITORY_ROOT / value.strip().strip("`")).resolve()
    try:
        candidate.relative_to(REPOSITORY_ROOT)
    except ValueError:
        return None
    return candidate


def parse_audit_contract(source: str | None) -> list[dict[str, str]]:
    """Parse a deliberately small Markdown contract; never execute free-form text."""
    checks = []
    for line_number, line in enumerate(str(source or "").splitlines(), start=1):
        match = re.match(r"^\s*-\s*(registry|source|validation)\s*:\s*(.+?)\s*$", line, re.IGNORECASE)
        if not match:
            continue
        checks.append({"kind": match.group(1).lower(), "value": match.group(2).strip(), "line": str(line_number)})
    return checks


def _run_validation(name: str) -> tuple[bool, str]:
    command = VALIDATION_COMMANDS.get(name)
    if not command:
        return False, f"Unsupported validation '{name}'."
    try:
        completed = subprocess.run(
            command,
            cwd=REPOSITORY_ROOT,
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            timeout=120,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        return False, f"Validation could not run: {error}"
    if completed.returncode == 0:
        return True, "Passed."
    output = (completed.stdout + completed.stderr).strip().splitlines()
    return False, output[-1] if output else f"Exited with code {completed.returncode}."


def _audit_registry(path_value: str) -> tuple[bool, str]:
    path = _repository_file(path_value)
    if not path or not path.is_file():
        return False, "Registered file does not exist inside the repository."
    relative_path = path.relative_to(REPOSITORY_ROOT).as_posix()
    try:
        completed = subprocess.run(
            [sys.executable, str(CODE_REGISTRY_TOOL), "search", path.name],
            cwd=REPOSITORY_ROOT,
            text=True,
            encoding="utf-8",
            errors="replace",
            capture_output=True,
            timeout=30,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as error:
        return False, f"Code Registry search could not run: {error}"
    if completed.returncode != 0:
        return False, "Code Registry search failed."
    return (relative_path in completed.stdout, "Registered and discoverable." if relative_path in completed.stdout else "File is absent from Code Registry search results.")


def _audit_source(value: str) -> tuple[bool, str]:
    path_value, separator, required_text = value.partition(" contains ")
    if not separator or not required_text.strip():
        return False, "Source checks must use '<relative path> contains <literal text>'."
    path = _repository_file(path_value)
    if not path or not path.is_file():
        return False, "Source file does not exist inside the repository."
    source = path.read_text(encoding="utf-8", errors="replace")
    literal = required_text.strip().strip("`")
    return (literal in source, "Required literal found." if literal in source else f"Required literal not found: {literal}")


def audit_contract(source: str | None) -> dict:
    checks = parse_audit_contract(source)
    results = []
    has_validation = False
    has_implementation_evidence = False

    for check in checks:
        kind, value = check["kind"], check["value"]
        if kind == "registry":
            has_implementation_evidence = True
            passed, detail = _audit_registry(value)
        elif kind == "source":
            has_implementation_evidence = True
            passed, detail = _audit_source(value)
        else:
            has_validation = True
            passed, detail = _run_validation(value)
        results.append({**check, "passed": passed, "detail": detail})

    configured = bool(checks)
    valid_contract = configured and has_validation and has_implementation_evidence
    if configured and not valid_contract:
        results.append({
            "kind": "contract", "value": "minimum evidence", "line": "0", "passed": False,
            "detail": "Require at least one registry/source check and one allowlisted validation.",
        })
    return {
        "configured": configured,
        "passed": valid_contract and all(result["passed"] for result in results),
        "results": results,
    }


def format_audit_report(result: dict, timestamp: str) -> str:
    if not result["configured"]:
        return f"Last audit: {timestamp}\nResult: NOT CONFIGURED\nAdd an Audit Contract before automatic status changes are allowed."
    outcome = "PASS" if result["passed"] else "FAIL"
    lines = [f"Last audit: {timestamp}", f"Result: {outcome}"]
    for item in result["results"]:
        state = "PASS" if item["passed"] else "FAIL"
        lines.append(f"- {state} [{item['kind']}] {item['value']} — {item['detail']}")
    return "\n".join(lines)
