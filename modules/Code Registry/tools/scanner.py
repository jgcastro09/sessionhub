#!/usr/bin/env python3
"""Repository discovery and non-destructive registry synchronization."""

from __future__ import annotations

import json
import os
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from registry_builder import load_registry_entries, write_modular_registries
from source_analyzer import analyze_file, humanize_identifier, primary_symbol


SEMANTIC_FIELDS = (
    "module", "description", "responsibilities", "criticality", "relatedFiles", "status",
)


def load_config(root: Path) -> dict[str, Any]:
    path = root / "Code Registry" / "config.json"
    return json.loads(path.read_text(encoding="utf-8"))


def _normalized_relative(root: Path, path: Path) -> str:
    return path.relative_to(root).as_posix()


def _now() -> str:
    return datetime.now(timezone.utc).replace(microsecond=0).isoformat().replace("+00:00", "Z")


def _path_starts_with(path: str, prefix: str) -> bool:
    return path.casefold().startswith(prefix.replace("\\", "/").casefold())


def _excluded_directory(relative_directory: str, name: str, config: dict[str, Any]) -> bool:
    if name in config.get("excludeDirectoryNames", {}):
        return True
    candidate = f"{relative_directory.rstrip('/')}/" if relative_directory else ""
    for prefix in config.get("excludePathPrefixes", {}):
        normalized = prefix.replace("\\", "/").rstrip("/") + "/"
        if candidate.casefold().startswith(normalized.casefold()):
            return True
    return False


def exclusion_reason(relative_path: str, config: dict[str, Any]) -> str | None:
    normalized = relative_path.replace("\\", "/")
    if normalized in config.get("excludeFiles", {}):
        return str(config["excludeFiles"][normalized])
    parts = normalized.split("/")
    for part in parts[:-1]:
        if part in config.get("excludeDirectoryNames", {}):
            return str(config["excludeDirectoryNames"][part])
    for prefix, reason in config.get("excludePathPrefixes", {}).items():
        if _path_starts_with(normalized, prefix):
            return str(reason)
    return None


def is_eligible(path: Path, relative_path: str, config: dict[str, Any]) -> bool:
    normalized = relative_path.replace("\\", "/")
    if normalized in config.get("explicitIncludeFiles", []):
        return True
    return (
        path.suffix.lower() in set(config.get("extensions", []))
        or path.name in set(config.get("exactFilenames", []))
    )


def discover_files(root: Path, config: dict[str, Any]) -> dict[str, Path]:
    discovered: dict[str, Path] = {}
    for scan_root_text in config.get("scanRoots", []):
        scan_root = (root / scan_root_text).resolve()
        if not scan_root.is_dir():
            continue
        for current, directories, filenames in os.walk(scan_root, topdown=True):
            current_path = Path(current)
            relative_directory = "" if current_path == root else _normalized_relative(root, current_path)
            directories[:] = sorted(
                (
                    name for name in directories
                    if not _excluded_directory(
                        f"{relative_directory}/{name}".strip("/"), name, config
                    )
                ),
                key=str.casefold,
            )
            for filename in sorted(filenames, key=str.casefold):
                path = current_path / filename
                relative_path = _normalized_relative(root, path)
                if exclusion_reason(relative_path, config):
                    continue
                if is_eligible(path, relative_path, config):
                    discovered[relative_path] = path

    for relative_path in config.get("explicitIncludeFiles", []):
        path = root / relative_path
        if path.is_file() and exclusion_reason(relative_path, config) is None:
            discovered[relative_path] = path
    return dict(sorted(discovered.items(), key=lambda item: item[0].casefold()))


def category_for(relative_path: str, path: Path, config: dict[str, Any]) -> str:
    extension = path.suffix.lower()
    for rule in config.get("categoryRules", []):
        if extension in set(rule.get("excludeExtensions", [])):
            continue
        prefix_match = any(_path_starts_with(relative_path, prefix) for prefix in rule.get("pathPrefixes", []))
        extension_match = extension in set(rule.get("extensions", []))
        exact_match = path.name in set(rule.get("exactFilenames", []))
        if prefix_match or extension_match or exact_match or rule.get("fallback"):
            return str(rule["category"])
    return "uncategorized"


def _title_segment(value: str) -> str:
    known = {
        "app": "Application",
        "core": "Core",
        "deck": "Live Deck",
        "engine": "Engine",
        "filesystem": "Filesystem",
        "graph": "Graph",
        "io": "I/O",
        "library_catalog": "Library Catalog",
        "mapper": "Mapping",
        "media": "Media",
        "midi": "MIDI",
        "nodes": "Nodes",
        "output": "Output",
        "paths": "Paths",
        "playback": "Playback",
        "presets": "Presets",
        "project": "Project",
        "recording": "Recording",
        "render": "Rendering",
        "scene": "Scene",
        "scenes": "Scenes",
        "services": "Services",
        "thumbnail": "Thumbnails",
        "timeline": "Timeline",
        "ui": "UI",
        "utils": "Utilities",
        "workspace": "Workspace",
        "webcontrol": "Web Control",
        "sync": "Sync",
        "blackmagic": "Blackmagic",
        "licensing": "Licensing",
        "syphon": "Syphon",
    }
    return known.get(value, humanize_identifier(value).title())


def module_for(relative_path: str) -> str:
    parts = relative_path.split("/")
    if relative_path.startswith("Code Registry/tools/"):
        return "Developer Tools/Code Registry"
    if relative_path.startswith("app/runtime_assets/.resources/web-control/"):
        return "Web Control"
    if relative_path.startswith("installer/"):
        if "/windows/" in f"/{relative_path}":
            return "Packaging/Windows"
        if "/macos/" in f"/{relative_path}":
            return "Packaging/macOS"
        return "Packaging/Validation"
    if relative_path.startswith("tools/agent/tests/"):
        return "Developer Tools/Agent Tests"
    if relative_path.startswith("tools/agent/"):
        return "Developer Tools/Agent Contracts"
    if relative_path.startswith("tools/ui-qa/"):
        return "Developer Tools/Visual QA"
    if relative_path.startswith("tools/"):
        return "Developer Tools/Build and Release"
    if relative_path.startswith("app/scripts/tests/"):
        return "Tests/Node Scaffolding"
    if relative_path.startswith("app/scripts/"):
        name = parts[-1].casefold()
        if any(token in name for token in ("build", "package", "sign", "stamp", "framework", "release")):
            return "Build/macOS"
        return "Developer Tools/App"
    if relative_path.startswith("app/tests/"):
        domain = _title_segment(parts[2]) if len(parts) > 2 else "Application"
        return f"Tests/{domain}"
    if relative_path.startswith("app/src/platform/"):
        suffix = _title_segment(parts[3]) if len(parts) > 4 else "Core"
        return f"Platform/{suffix}"
    if relative_path.startswith("app/src/nodes/modules/"):
        owner = _title_segment(parts[4]) if len(parts) > 5 else "Modules"
        return f"Nodes/{owner}"
    if relative_path.startswith("app/src/nodes/library/"):
        owner = _title_segment(parts[4]) if len(parts) > 5 else "Library"
        return f"Nodes/{owner}"
    if relative_path.startswith("app/src/nodes/"):
        return "Nodes/Core"
    if relative_path.startswith("app/src/services/"):
        owner = _title_segment(parts[3]) if len(parts) > 4 else "Core"
        return f"Services/{owner}"
    if relative_path.startswith("app/src/core/license/"):
        return "Core/Licensing"
    if relative_path.startswith("app/src/media/hap/"):
        return "Media/Hap"
    if relative_path in {"app/src/main.cpp", "app/src/icon.rc"}:
        return "Application" if relative_path.endswith("main.cpp") else "Build/Windows"
    if relative_path.startswith("app/src/"):
        owner = _title_segment(parts[2]) if len(parts) > 2 else "Application"
        return owner
    if relative_path.startswith("app/"):
        if relative_path.endswith((".sln", ".vcxproj", ".filters", ".props", ".rc")):
            return "Build/Windows"
        return "Build/openFrameworks"
    return "Repository Tools"


def _semantic_subject(analysis: dict[str, Any]) -> str:
    return humanize_identifier(primary_symbol(analysis)).strip() or humanize_identifier(Path(analysis["filename"]).stem)


def _description_for(relative_path: str, analysis: dict[str, Any], module: str) -> str:
    subject = _semantic_subject(analysis)
    kind = analysis["kind"]
    name = analysis["filename"].casefold()
    special = {
        "app/src/main.cpp": "Inicializa o processo nativo e transfere o controle para o aplicativo NodeStage.",
        "app/runtime_assets/.resources/web-control/app.js": "Implementa a interface remota, o transporte e as interações do Web Control.",
        "app/runtime_assets/.resources/web-control/index.html": "Define o documento inicial carregado pelo Web Control.",
        "app/runtime_assets/.resources/web-control/styles.css": "Define o layout e os estados visuais do Web Control.",
        "app/runtime_assets/.resources/web-control/sw.js": "Implementa o service worker e a política de cache do Web Control.",
        "Code Registry/tools/code_registry.py": "Expõe a interface de linha de comando oficial do Code Registry.",
        "Code Registry/tools/scanner.py": "Descobre código elegível e sincroniza dados automáticos sem apagar metadados semânticos.",
        "Code Registry/tools/validator.py": "Valida cobertura, schema, hashes, revisões, classificações e artefatos consolidados do Code Registry.",
        "Code Registry/tools/registry_builder.py": "Grava registros modulares e gera os payloads consolidados e de auditoria de forma atômica.",
        "Code Registry/tools/source_analyzer.py": "Extrai metadados estruturais leves de C++, Python, Web, shaders e scripts de build.",
    }
    if relative_path in special:
        return special[relative_path]
    if kind == "header":
        return f"Define o contrato de {subject} usado pelo módulo {module}."
    if kind == "test":
        tested = humanize_identifier(Path(analysis["filename"]).stem.removeprefix("test_"))
        return f"Verifica {tested} para preservar o contrato de {module}."
    if kind == "tool":
        stem = Path(analysis["filename"]).stem
        actions = (
            ("audit_", "Audita"),
            ("validate_", "Valida"),
            ("update_", "Atualiza"),
            ("scaffold_", "Cria o scaffold de"),
            ("capture_", "Captura"),
            ("crop_", "Recorta"),
            ("report_", "Gera o relatório de"),
        )
        for prefix, verb in actions:
            if stem.startswith(prefix):
                target = humanize_identifier(stem.removeprefix(prefix))
                return f"{verb} {target} no fluxo de {module}."
        return f"Implementa a ferramenta {subject} para o fluxo de {module}."
    if kind == "component":
        return f"Define o componente React {subject} da interface {module}."
    if kind == "stylesheet":
        return f"Define o layout e os estilos visuais de {module}."
    if kind == "document":
        return f"Define o documento base consumido por {module}."
    if kind == "service-worker":
        return f"Controla cache e atualização offline de {module}."
    if kind in {"module", "source"}:
        action = "configura" if name.endswith("config.js") else "implementa"
        return f"{action.capitalize()} {subject} para {module}."
    if "shader" in kind:
        return f"Implementa o estágio gráfico {subject} usado por {module}."
    if kind in {"build-script", "build-manifest", "resource-script"}:
        return f"Define e automatiza {subject} no fluxo de {module}."

    suffix_actions = (
        ("InputGenerator", "captura a fonte e produz frames para"),
        ("Generator", "gera frames ou dados visuais para"),
        ("Effect", "aplica processamento visual em"),
        ("Renderer", "compõe e desenha dados de"),
        ("Compositor", "combina fontes e camadas de"),
        ("Serializer", "serializa e restaura o estado de"),
        ("Registry", "cataloga e resolve itens de"),
        ("Factory", "cria implementações selecionadas por"),
        ("Verifier", "verifica provas e invariantes de"),
        ("Validator", "valida contratos de"),
        ("Analyzer", "analisa sinais e dados de"),
        ("Importer", "importa recursos para"),
        ("Exporter", "exporta dados e frames de"),
        ("Encoder", "codifica mídia produzida por"),
        ("Player", "reproduz mídia usada por"),
        ("Storage", "persiste e restaura dados de"),
        ("Cache", "mantém dados reutilizáveis de"),
        ("Queue", "ordena trabalho assíncrono de"),
        ("Repository", "armazena e consulta definições de"),
        ("Protocol", "codifica e interpreta mensagens de"),
        ("Controller", "coordena comandos e estado de"),
        ("Manager", "gerencia ciclo de vida e estado de"),
        ("Service", "coordena operações de"),
        ("Panel", "renderiza e opera a interface de"),
        ("Modal", "apresenta e processa o diálogo de"),
        ("Widget", "renderiza o controle de interface de"),
        ("Overlay", "desenha informações sobre"),
        ("Output", "publica frames e estado de"),
        ("Node", "executa o nó de patch de"),
        ("Bridge", "adapta a integração nativa de"),
        ("Client", "consome o serviço de"),
        ("Server", "expõe o serviço local de"),
        ("Driver", "controla a comunicação de baixo nível de"),
        ("Model", "mantém o modelo de dados de"),
        ("State", "mantém o estado persistente ou transitório de"),
        ("Selection", "mantém a seleção ativa de"),
        ("Transport", "controla tempo e reprodução de"),
        ("Clock", "fornece tempo e sincronização para"),
        ("Loader", "carrega recursos usados por"),
        ("Resolver", "resolve fontes e referências de"),
        ("Settings", "define configurações de"),
        ("Types", "implementa tipos auxiliares de"),
    )
    raw_subject = primary_symbol(analysis)
    for suffix, action in suffix_actions:
        if raw_subject.endswith(suffix) or Path(analysis["filename"]).stem.endswith(suffix):
            return f"Implementa {subject}, que {action} {module}."
    return f"Implementa as operações de {subject} no módulo {module}."


def _responsibilities_for(analysis: dict[str, Any], module: str) -> list[str]:
    subject = _semantic_subject(analysis)
    kind = analysis["kind"]
    if kind == "header":
        return [f"Definir o contrato de {subject}", "Expor tipos e operações públicas"]
    if kind == "test":
        return [f"Verificar {subject}", f"Detectar regressões em {module}"]
    if kind == "stylesheet":
        return [f"Estilizar {module}", "Definir layout e estados visuais"]
    if kind == "document":
        return [f"Estruturar {module}", "Carregar recursos mantidos pela interface"]
    if kind == "component":
        return [f"Renderizar {subject}", f"Conectar interações de {module}"]
    if "shader" in kind:
        return [f"Processar o estágio {subject}", "Produzir a saída gráfica"]
    if kind in {"build-script", "build-manifest", "resource-script"}:
        return [f"Automatizar {subject}", f"Preservar o fluxo de {module}"]
    if kind == "tool":
        return [f"Executar {subject}", f"Validar o fluxo de {module}"]
    return [f"Implementar {subject}", f"Integrar o comportamento a {module}"]


def _criticality_for(relative_path: str, module: str) -> str:
    text = f"{relative_path} {module}".casefold()
    if any(token in text for token in ("licens", "signature", "securestorage", "machinefingerprint", "releasemanifest")):
        return "critical"
    if any(token in text for token in ("render", "output", "projectserializer", "change_contract", "vcxproj", "installer")):
        return "important"
    return "standard"


def _probable_relations(entries: list[dict[str, Any]]) -> dict[str, list[str]]:
    by_stem: dict[str, list[str]] = defaultdict(list)
    for entry in entries:
        stem = Path(entry["filename"]).stem.casefold()
        by_stem[stem].append(entry["path"])
    result: dict[str, list[str]] = {}
    for entry in entries:
        stem = Path(entry["filename"]).stem.casefold()
        result[entry["path"]] = sorted(
            (path for path in by_stem[stem] if path != entry["path"]), key=str.casefold
        )
    return result


def scan_repository(root: Path, config: dict[str, Any]) -> tuple[list[dict[str, Any]], dict[str, list[Any]]]:
    discovered = discover_files(root, config)
    existing, load_errors = load_registry_entries(root, config)
    if load_errors and any((root / "Code Registry" / path).exists() for path in config["registryFiles"].values()):
        raise RuntimeError("; ".join(load_errors))
    initial_bootstrap = not existing
    existing_by_path = {entry["path"]: entry for entry in existing}
    missing_existing = {path: entry for path, entry in existing_by_path.items() if path not in discovered}
    missing_by_hash: dict[str, list[dict[str, Any]]] = defaultdict(list)
    for entry in missing_existing.values():
        missing_by_hash[str(entry.get("hash", ""))].append(entry)

    timestamp = _now()
    entries: list[dict[str, Any]] = []
    events: dict[str, list[Any]] = {"new": [], "changed": [], "moved": [], "removed": []}
    moved_old_paths: set[str] = set()
    for relative_path, path in discovered.items():
        automatic = analyze_file(path, relative_path)
        automatic["category"] = category_for(relative_path, path, config)
        previous = existing_by_path.get(relative_path)
        moved_from: str | None = None
        if previous is None:
            candidates = [item for item in missing_by_hash.get(automatic["hash"], []) if item["path"] not in moved_old_paths]
            if len(candidates) == 1:
                previous = candidates[0]
                moved_from = previous["path"]
                moved_old_paths.add(moved_from)

        if previous is None:
            module = module_for(relative_path)
            reviewed_hash = automatic["hash"] if initial_bootstrap else ""
            entry = {
                **automatic,
                "module": module,
                "description": _description_for(relative_path, automatic, module),
                "responsibilities": _responsibilities_for(automatic, module),
                "criticality": _criticality_for(relative_path, module),
                "relatedFiles": [],
                "probableRelatedFiles": [],
                "previousPaths": [],
                "lastReviewedHash": reviewed_hash,
                "reviewStatus": "reviewed" if initial_bootstrap else "needs_review",
                "status": "active",
                "lastScannedAt": timestamp,
            }
            events["new"].append(relative_path)
        else:
            entry = {**automatic}
            for field in SEMANTIC_FIELDS:
                entry[field] = previous.get(field)
            entry["previousPaths"] = list(previous.get("previousPaths", []))
            if moved_from:
                entry["previousPaths"] = sorted({*entry["previousPaths"], moved_from}, key=str.casefold)
            entry["probableRelatedFiles"] = list(previous.get("probableRelatedFiles", []))
            content_changed = automatic["hash"] != previous.get("hash")
            automatic_changed = any(
                automatic.get(field) != previous.get(field)
                for field in (
                    "filename", "extension", "language", "kind", "category", "dependencies",
                    "includes", "imports", "exports", "symbols", "lineCount", "sizeBytes",
                )
            )
            entry["lastReviewedHash"] = previous.get("lastReviewedHash", "")
            entry["reviewStatus"] = previous.get("reviewStatus", "needs_review")
            if moved_from or content_changed:
                entry["reviewStatus"] = "needs_review"
            entry["lastScannedAt"] = timestamp if (moved_from or content_changed or automatic_changed) else previous.get("lastScannedAt", timestamp)
            if content_changed:
                events["changed"].append(relative_path)
            if moved_from:
                events["moved"].append({"from": moved_from, "to": relative_path})
        entries.append(entry)

    events["removed"] = sorted(
        (path for path in missing_existing if path not in moved_old_paths), key=str.casefold
    )
    relations = _probable_relations(entries)
    for entry in entries:
        entry["probableRelatedFiles"] = relations[entry["path"]]
        if initial_bootstrap:
            entry["relatedFiles"] = list(relations[entry["path"]])
        entry["responsibilities"] = list(entry.get("responsibilities") or [])[: int(config["maximumResponsibilities"])]

    entries.sort(key=lambda item: item["path"].casefold())
    for key in ("new", "changed", "removed"):
        events[key] = sorted(events[key], key=str.casefold)
    events["moved"] = sorted(events["moved"], key=lambda item: item["to"].casefold())
    write_modular_registries(root, config, entries)
    return entries, events
