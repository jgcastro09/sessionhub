#!/usr/bin/env python3
"""Lightweight, standard-library source analysis for the NodeStage Code Registry."""

from __future__ import annotations

import ast
import hashlib
import json
import re
from datetime import datetime, timezone
from pathlib import Path
from typing import Any


CPP_EXTENSIONS = {".c", ".cc", ".cpp", ".cxx", ".h", ".hh", ".hpp", ".hxx"}
HEADER_EXTENSIONS = {".h", ".hh", ".hpp", ".hxx"}
WEB_EXTENSIONS = {
    ".html", ".htm", ".css", ".scss", ".sass", ".less", ".js", ".mjs",
    ".cjs", ".jsx", ".ts", ".tsx",
}
SHADER_EXTENSIONS = {
    ".glsl", ".vert", ".frag", ".geom", ".tesc", ".tese", ".comp",
    ".hlsl", ".fx", ".metal",
}
BUILD_EXTENSIONS = {
    ".cmake", ".ps1", ".bat", ".cmd", ".sh", ".command", ".make", ".sln",
    ".vcxproj", ".filters", ".props", ".rc", ".iss",
}

LANGUAGE_BY_EXTENSION = {
    ".c": "C",
    ".cc": "C++",
    ".cpp": "C++",
    ".cxx": "C++",
    ".h": "C++",
    ".hh": "C++",
    ".hpp": "C++",
    ".hxx": "C++",
    ".m": "Objective-C",
    ".mm": "Objective-C++",
    ".py": "Python",
    ".html": "HTML",
    ".htm": "HTML",
    ".css": "CSS",
    ".scss": "SCSS",
    ".sass": "Sass",
    ".less": "Less",
    ".js": "JavaScript",
    ".mjs": "JavaScript",
    ".cjs": "JavaScript",
    ".jsx": "JavaScript React",
    ".ts": "TypeScript",
    ".tsx": "TypeScript React",
    ".glsl": "GLSL",
    ".vert": "GLSL",
    ".frag": "GLSL",
    ".geom": "GLSL",
    ".tesc": "GLSL",
    ".tese": "GLSL",
    ".comp": "GLSL",
    ".hlsl": "HLSL",
    ".fx": "HLSL",
    ".metal": "Metal Shading Language",
    ".cmake": "CMake",
    ".ps1": "PowerShell",
    ".bat": "Batch",
    ".cmd": "Batch",
    ".sh": "Shell",
    ".command": "Shell",
    ".make": "Make",
    ".sln": "MSBuild",
    ".vcxproj": "MSBuild",
    ".filters": "MSBuild",
    ".props": "MSBuild",
    ".rc": "Windows Resource Script",
    ".iss": "Inno Setup",
    ".json": "JSON Build Manifest",
}

EXACT_LANGUAGE = {
    "CMakeLists.txt": "CMake",
    "Makefile": "Make",
    "package.json": "JSON Build Manifest",
}

CPP_FUNCTION_RE = re.compile(
    r"(?m)^\s*(?:template\s*<[^;{]+>\s*)?"
    r"(?:[A-Za-z_][\w:<>,\s*&~]*?\s+)?"
    r"([A-Za-z_~][\w]*(?:::[A-Za-z_~][\w]*)*)\s*"
    r"\([^;{}]*\)\s*(?:const\s*)?(?:noexcept\s*)?(?:override\s*)?(?:\{|$)"
)
CPP_KEYWORDS = {"if", "for", "while", "switch", "catch", "return", "sizeof"}


def _unique(values: list[str]) -> list[str]:
    return sorted({value.strip() for value in values if value and value.strip()}, key=str.casefold)


def _read_source(path: Path) -> tuple[bytes, str]:
    payload = path.read_bytes()
    try:
        text = payload.decode("utf-8")
    except UnicodeDecodeError:
        text = payload.decode("utf-8", errors="replace")
    return payload, text


def language_for(path: Path) -> str:
    return EXACT_LANGUAGE.get(path.name, LANGUAGE_BY_EXTENSION.get(path.suffix.lower(), "Unknown"))


def kind_for(path: Path, relative_path: str) -> str:
    extension = path.suffix.lower()
    name = path.name.lower()
    relative_lower = relative_path.lower()
    if extension in HEADER_EXTENSIONS:
        return "header"
    if extension in {".c", ".cc", ".cpp", ".cxx", ".m", ".mm"}:
        return "implementation"
    if extension == ".py":
        return "test" if "/tests/" in f"/{relative_lower}" or name.startswith("test_") else "tool"
    if extension in {".html", ".htm"}:
        return "document"
    if extension in {".css", ".scss", ".sass", ".less"}:
        return "stylesheet"
    if extension in {".jsx", ".tsx"}:
        return "component"
    if extension in {".js", ".mjs", ".cjs", ".ts"}:
        return "service-worker" if name in {"sw.js", "service-worker.js"} else "module"
    if extension in SHADER_EXTENSIONS:
        return {
            ".vert": "vertex-shader",
            ".frag": "fragment-shader",
            ".geom": "geometry-shader",
            ".tesc": "tessellation-control-shader",
            ".tese": "tessellation-evaluation-shader",
            ".comp": "compute-shader",
        }.get(extension, "shader")
    if extension == ".rc":
        return "resource-script"
    if path.name in {"package.json", "Makefile", "CMakeLists.txt"} or extension in {
        ".sln", ".vcxproj", ".filters", ".props", ".make"
    }:
        return "build-manifest"
    if extension in BUILD_EXTENSIONS:
        return "build-script"
    return "source"


def _analyze_cpp(text: str) -> dict[str, Any]:
    includes = _unique(re.findall(r'^\s*#\s*include\s*[<"]([^>"]+)[>"]', text, re.MULTILINE))
    namespaces = _unique(re.findall(r"\bnamespace\s+([A-Za-z_]\w*(?:::\w+)*)\s*\{", text))
    classes = re.findall(r"\bclass\s+(?:[A-Z_][A-Z0-9_]*\s+)*([A-Za-z_]\w*)\b", text)
    classes += re.findall(r"@(interface|implementation)\s+([A-Za-z_]\w*)", text)
    normalized_classes = [item[1] if isinstance(item, tuple) else item for item in classes]
    structs = _unique(re.findall(r"\bstruct\s+([A-Za-z_]\w*)\b", text))
    enums = _unique(re.findall(r"\benum(?:\s+class)?\s+([A-Za-z_]\w*)\b", text))
    functions = []
    for match in CPP_FUNCTION_RE.finditer(text):
        value = match.group(1)
        if value.split("::")[-1] not in CPP_KEYWORDS:
            functions.append(value)
    return {
        "includes": includes,
        "imports": [],
        "exports": [],
        "symbols": {
            "namespaces": namespaces,
            "classes": _unique(normalized_classes),
            "structs": structs,
            "enums": enums,
            "functions": _unique(functions),
        },
    }


def _analyze_python(text: str) -> dict[str, Any]:
    imports: list[str] = []
    classes: list[str] = []
    functions: list[str] = []
    exports: list[str] = []
    try:
        tree = ast.parse(text)
        for node in ast.walk(tree):
            if isinstance(node, ast.Import):
                imports.extend(alias.name for alias in node.names)
            elif isinstance(node, ast.ImportFrom):
                prefix = "." * node.level + (node.module or "")
                imports.append(prefix)
            elif isinstance(node, ast.ClassDef):
                classes.append(node.name)
            elif isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef)):
                functions.append(node.name)
            elif isinstance(node, ast.Assign):
                if any(isinstance(target, ast.Name) and target.id == "__all__" for target in node.targets):
                    if isinstance(node.value, (ast.List, ast.Tuple)):
                        exports.extend(
                            element.value
                            for element in node.value.elts
                            if isinstance(element, ast.Constant) and isinstance(element.value, str)
                        )
    except SyntaxError:
        imports = re.findall(r"^\s*(?:from\s+([\w.]+)|import\s+([\w.]+))", text, re.MULTILINE)
        imports = [left or right for left, right in imports]
        classes = re.findall(r"^\s*class\s+([A-Za-z_]\w*)", text, re.MULTILINE)
        functions = re.findall(r"^\s*(?:async\s+)?def\s+([A-Za-z_]\w*)", text, re.MULTILINE)
    return {
        "includes": [],
        "imports": _unique(imports),
        "exports": _unique(exports),
        "symbols": {
            "classes": _unique(classes),
            "functions": _unique(functions),
        },
    }


def _analyze_web(text: str, extension: str) -> dict[str, Any]:
    imports = re.findall(r"\bfrom\s+['\"]([^'\"]+)['\"]", text)
    imports += re.findall(r"\brequire\s*\(\s*['\"]([^'\"]+)['\"]\s*\)", text)
    if extension in {".html", ".htm"}:
        imports += re.findall(r"<(?:script|link)[^>]+(?:src|href)=['\"]([^'\"]+)['\"]", text, re.IGNORECASE)
    classes = re.findall(r"\bclass\s+([A-Za-z_$][\w$]*)", text)
    functions = re.findall(r"\bfunction\s+([A-Za-z_$][\w$]*)\s*\(", text)
    functions += re.findall(r"\b(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*(?:async\s*)?\([^)]*\)\s*=>", text)
    exports = re.findall(r"\bexport\s+(?:default\s+)?(?:class|function|const|let|var)?\s*([A-Za-z_$][\w$]*)", text)
    components = [name for name in functions + classes if name[:1].isupper()]
    selectors: list[str] = []
    if extension in {".css", ".scss", ".sass", ".less"}:
        selectors = re.findall(r"(?m)^\s*([.#][A-Za-z_-][\w-]*)[^,{]*\s*\{", text)
    return {
        "includes": [],
        "imports": _unique(imports),
        "exports": _unique(exports),
        "symbols": {
            "classes": _unique(classes),
            "components": _unique(components),
            "functions": _unique(functions),
            "selectors": _unique(selectors),
        },
    }


def _analyze_shader(text: str) -> dict[str, Any]:
    includes = _unique(re.findall(r'^\s*#\s*include\s*[<"]([^>"]+)[>"]', text, re.MULTILINE))
    uniforms = re.findall(r"\buniform\s+\w+(?:\s*<[^>]+>)?\s+([A-Za-z_]\w*)", text)
    inputs = re.findall(r"(?:^|\s)(?:in|attribute)\s+\w+\s+([A-Za-z_]\w*)", text)
    outputs = re.findall(r"(?:^|\s)out\s+\w+\s+([A-Za-z_]\w*)", text)
    functions = re.findall(r"\b(?:void|float|vec[234]|mat[234]|half\d?|float\d?)\s+([A-Za-z_]\w*)\s*\(", text)
    return {
        "includes": includes,
        "imports": [],
        "exports": [],
        "symbols": {
            "uniforms": _unique(uniforms),
            "inputs": _unique(inputs),
            "outputs": _unique(outputs),
            "functions": _unique(functions),
        },
    }


def _analyze_build(text: str, path: Path) -> dict[str, Any]:
    functions = re.findall(r"(?mi)^\s*function\s+([A-Za-z_][\w-]*)", text)
    functions += re.findall(r"(?m)^\s*([A-Za-z_][\w-]*)\s*\(\)\s*\{", text)
    targets = re.findall(r"(?mi)<Target\s+Name=['\"]([^'\"]+)['\"]", text)
    if path.name == "Makefile" or path.suffix.lower() == ".make":
        targets += re.findall(r"(?m)^([A-Za-z0-9_.-]+)\s*:(?![=])", text)
    scripts: list[str] = []
    if path.name == "package.json":
        try:
            data = json.loads(text)
            scripts = sorted((data.get("scripts") or {}).keys())
        except json.JSONDecodeError:
            scripts = []
    return {
        "includes": [],
        "imports": [],
        "exports": [],
        "symbols": {
            "functions": _unique(functions),
            "targets": _unique(targets),
            "scripts": _unique(scripts),
        },
    }


def analyze_file(path: Path, relative_path: str) -> dict[str, Any]:
    payload, text = _read_source(path)
    last_modified_at = datetime.fromtimestamp(path.stat().st_mtime, timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")
    extension = path.suffix.lower()
    language = language_for(path)
    if extension in CPP_EXTENSIONS or extension in {".m", ".mm"}:
        details = _analyze_cpp(text)
    elif extension == ".py":
        details = _analyze_python(text)
    elif extension in WEB_EXTENSIONS:
        details = _analyze_web(text, extension)
    elif extension in SHADER_EXTENSIONS:
        details = _analyze_shader(text)
    else:
        details = _analyze_build(text, path)

    includes = details["includes"]
    imports = details["imports"]
    return {
        "path": relative_path,
        "filename": path.name,
        "extension": extension,
        "language": language,
        "kind": kind_for(path, relative_path),
        "dependencies": _unique([*includes, *imports]),
        "includes": includes,
        "imports": imports,
        "exports": details["exports"],
        "symbols": details["symbols"],
        "lineCount": text.count("\n") + (1 if text and not text.endswith("\n") else 0),
        "sizeBytes": len(payload),
        "lastModifiedAt": last_modified_at,
        "hash": hashlib.sha256(payload).hexdigest(),
    }


def humanize_identifier(value: str) -> str:
    value = re.sub(r"[_-]+", " ", value)
    value = re.sub(r"(?<=[a-z0-9])(?=[A-Z])", " ", value)
    value = re.sub(r"(?<=[A-Z])(?=[A-Z][a-z])", " ", value)
    return " ".join(value.split())


def primary_symbol(analysis: dict[str, Any]) -> str:
    symbols = analysis.get("symbols", {})
    stem = Path(analysis["filename"]).stem
    if analysis.get("kind") in {
        "header", "implementation", "test", "tool", "stylesheet", "document",
        "service-worker", "build-script", "build-manifest", "resource-script",
    }:
        return stem
    candidates: list[str] = []
    for key in ("classes", "components", "structs", "functions", "scripts", "targets"):
        values = symbols.get(key) or []
        candidates.extend(str(value).split("::")[-1] for value in values)
    exact = next((value for value in candidates if value.casefold() == stem.casefold()), None)
    if exact:
        return exact
    return candidates[0] if candidates else stem
