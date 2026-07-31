#!/usr/bin/env python3
"""Task Board CLI backed by one canonical Markdown file per Card."""

import argparse
import datetime
import json
import sys
from pathlib import Path
from token_metrics import calculate_markdown_token_metrics

if hasattr(sys.stdout, "reconfigure"):
    sys.stdout.reconfigure(encoding="utf-8")
if hasattr(sys.stderr, "reconfigure"):
    sys.stderr.reconfigure(encoding="utf-8")

SCRIPT_DIR = Path(__file__).resolve().parent
TASK_BOARD_DIR = SCRIPT_DIR.parent
CARDS_DIR = TASK_BOARD_DIR / "data" / "cards"

VALID_TYPES = ["Ideia", "Implementação", "Melhoria", "Ajuste", "Bug", "Correção"]
VALID_STATUSES = ["Ideias", "Pendente", "Em andamento", "Ajuste necessário", "Implementado", "Arquivado"]
VALID_PRIORITIES = ["Baixa", "Média", "Alta", "Crítica"]

METADATA_KEYS = [
    "id", "title", "summary", "type", "status", "priority", "created_at",
    "updated_at", "completed_at",
]
BODY_SECTIONS = [
    ("description", "Detailed Description", "description"),
    ("ai_prompt", "Detailed AI Prompt", "ai-prompt"),
    ("expected_features", "Expected Features", "expected-features"),
    ("audit_contract", "Audit Contract", "audit-contract"),
    ("audit_report", "Audit Report", "audit-report"),
    ("completion_summary", "Completion Summary", "completion-summary"),
    ("notes_and_issues", "Notes and Issues", "notes-and-issues"),
]


def get_now_formatted():
    now = datetime.datetime.now()
    return now.strftime("%d/%m/%Y %H:%M (America/Sao_Paulo)")


def ensure_cards_dir():
    CARDS_DIR.mkdir(parents=True, exist_ok=True)


def normalize_line(value):
    return " ".join(str(value or "").replace("\r", "").replace("\n", " ").split())


def normalize_body(value):
    return str(value or "").replace("\r\n", "\n").replace("\r", "\n").strip()


def metadata_value(value):
    return normalize_line(value) if value else ""


def serialize_card(card):
    lines = ["---"]
    for key in METADATA_KEYS:
        lines.append(f"{key}: {metadata_value(card.get(key))}")
    lines.append("impacted_areas:")
    for area in card.get("impacted_areas", []):
        lines.append(f"  - {normalize_line(area)}")
    lines.extend(["---", "", f"# {card.get('id', 'TASK')} — {card.get('title', '')}", ""])
    if card.get("summary"):
        lines.extend(["## Summary", "", normalize_body(card["summary"]), ""])

    for field, heading, marker in BODY_SECTIONS:
        value = normalize_body(card.get(field))
        if not value and field in {"completion_summary", "notes_and_issues"}:
            continue
        lines.extend([
            f"## {heading}", "", f"<!-- task-board:{marker}:start -->",
            value, f"<!-- task-board:{marker}:end -->", "",
        ])
    return "\n".join(lines).rstrip() + "\n"


def parse_frontmatter(source):
    if not source.startswith("---\n"):
        raise ValueError("missing Markdown frontmatter")
    end = source.find("\n---\n", 4)
    if end < 0:
        raise ValueError("unterminated Markdown frontmatter")

    metadata = {"impacted_areas": []}
    current_list = None
    for line in source[4:end].splitlines():
        if line.startswith("  - ") and current_list:
            metadata[current_list].append(line[4:].strip())
            continue
        key, separator, value = line.partition(":")
        if not separator:
            continue
        key, value = key.strip(), value.strip()
        if key == "impacted_areas":
            metadata[key] = []
            current_list = key
        else:
            metadata[key] = value or None
            current_list = None
    return metadata, source[end + 5:]


def extract_section(body, marker):
    start_marker = f"<!-- task-board:{marker}:start -->"
    end_marker = f"<!-- task-board:{marker}:end -->"
    start = body.find(start_marker)
    if start < 0:
        return None
    start += len(start_marker)
    end = body.find(end_marker, start)
    if end < 0:
        raise ValueError(f"unterminated {marker} section")
    return body[start:end].strip() or None


def parse_card_markdown(source):
    metadata, body = parse_frontmatter(source)
    if not metadata.get("id") or not metadata.get("title"):
        raise ValueError("frontmatter must contain id and title")
    card = {
        "id": metadata["id"],
        "title": metadata["title"],
        "summary": metadata.get("summary") or metadata["title"],
        "description": extract_section(body, "description") or "",
        "ai_prompt": extract_section(body, "ai-prompt") or "",
        "expected_features": extract_section(body, "expected-features") or "",
        "audit_contract": extract_section(body, "audit-contract") or "",
        "audit_report": extract_section(body, "audit-report") or "",
        "impacted_areas": metadata.get("impacted_areas", []),
        "type": metadata.get("type") or "Implementação",
        "status": metadata.get("status") or "Pendente",
        "priority": metadata.get("priority") or "Média",
        "created_at": metadata.get("created_at"),
        "updated_at": metadata.get("updated_at"),
        "completed_at": metadata.get("completed_at"),
        "completion_summary": extract_section(body, "completion-summary"),
        "notes_and_issues": extract_section(body, "notes-and-issues"),
    }
    return card


def read_card_file(card_file):
    source = card_file.read_text(encoding="utf-8")
    card = parse_card_markdown(source)
    card["markdown_token_metrics"] = calculate_markdown_token_metrics(source)
    return card


def load_all_cards():
    ensure_cards_dir()
    cards = []
    for file_path in CARDS_DIR.glob("TASK-*.md"):
        try:
            cards.append(read_card_file(file_path))
        except (OSError, ValueError) as error:
            print(f"[Warning] Failed to load card {file_path.name}: {error}", file=sys.stderr)
    return sorted(cards, key=lambda card: int(card.get("id", "TASK-0").replace("TASK-", "") or 0))


def normalize_card_id(card_id):
    card_id = card_id.upper()
    return card_id if card_id.startswith("TASK-") else f"TASK-{int(card_id):04d}"


def load_card_by_id(card_id):
    normalized_id = normalize_card_id(card_id)
    card_file = CARDS_DIR / f"{normalized_id}.md"
    return (read_card_file(card_file), card_file) if card_file.exists() else (None, card_file)


def save_card(card_data):
    ensure_cards_dir()
    card_file = CARDS_DIR / f"{card_data['id']}.md"
    temporary_file = card_file.with_suffix(".md.tmp")
    temporary_file.write_text(serialize_card(card_data), encoding="utf-8", newline="\n")
    temporary_file.replace(card_file)
    return card_file


def get_next_id():
    cards = load_all_cards()
    return f"TASK-{max([int(card['id'].replace('TASK-', '')) for card in cards] or [0]) + 1:04d}"


def migrate_json_cards(apply):
    ensure_cards_dir()
    legacy_files = sorted(CARDS_DIR.glob("TASK-*.json"))
    migration = []
    for legacy_file in legacy_files:
        destination = legacy_file.with_suffix(".md")
        if destination.exists():
            raise ValueError(f"destination already exists: {destination.name}")
        card = json.loads(legacy_file.read_text(encoding="utf-8"))
        migration.append((legacy_file, destination, card))

    if not apply:
        return migration
    for legacy_file, _, card in migration:
        save_card(card)
        legacy_file.unlink()
    return migration


def cmd_list(args):
    cards = load_all_cards()
    if args.status:
        cards = [card for card in cards if card.get("status", "").lower() == args.status.lower()]
    if args.type:
        cards = [card for card in cards if card.get("type", "").lower() == args.type.lower()]
    if args.priority:
        cards = [card for card in cards if card.get("priority", "").lower() == args.priority.lower()]
    if args.json:
        print(json.dumps(cards, ensure_ascii=False, indent=2))
        return
    for card in cards:
        print(f"{card['id']}: {card['status']} | {card['type']} | {card['priority']} | {card['title']}")
    print(f"Total: {len(cards)}")


def cmd_search(args):
    query = args.query.lower()
    cards = [card for card in load_all_cards() if query in " ".join([
        str(card.get(key, "")) for key in ("id", "title", "summary", "description", "ai_prompt", "expected_features", "completion_summary", "notes_and_issues")
    ] + card.get("impacted_areas", [])).lower()]
    if args.json:
        print(json.dumps(cards, ensure_ascii=False, indent=2))
        return
    for card in cards:
        print(f"{card['id']}: {card['title']}")
    print(f"Total: {len(cards)}")


def cmd_show(args):
    card, card_file = load_card_by_id(args.id)
    if not card:
        raise SystemExit(f"Card {args.id} not found.")
    if args.json:
        print(json.dumps(card, ensure_ascii=False, indent=2))
        return
    print(card_file.read_text(encoding="utf-8"), end="")


def cmd_create(args):
    now = get_now_formatted()
    card = {
        "id": get_next_id(), "title": args.title, "summary": args.summary or args.title,
        "description": args.description or args.summary or args.title, "ai_prompt": args.prompt or "",
        "expected_features": args.expected or "", "impacted_areas": [area.strip() for area in (args.areas or "").split(",") if area.strip()],
        "type": args.type, "status": args.status, "priority": args.priority,
        "created_at": now, "updated_at": now, "completed_at": now if args.status == "Implementado" else None,
        "completion_summary": args.completion_summary if args.status == "Implementado" else None, "notes_and_issues": None,
    }
    print(f"Card {card['id']} created: {save_card(card).name}")


def cmd_status(args):
    card, _ = load_card_by_id(args.id)
    if not card:
        raise SystemExit(f"Card {args.id} not found.")
    card["status"], card["updated_at"] = args.status, get_now_formatted()
    if args.status == "Implementado" and args.summary:
        card["completed_at"], card["completion_summary"] = card["updated_at"], args.summary
    if args.status == "Ajuste necessário" and args.summary:
        card["notes_and_issues"] = args.summary
    save_card(card)


def cmd_complete(args):
    card, _ = load_card_by_id(args.id)
    if not card:
        raise SystemExit(f"Card {args.id} not found.")
    now = get_now_formatted()
    card.update(status="Implementado", updated_at=now, completed_at=now, completion_summary=args.summary)
    save_card(card)


def cmd_reopen(args):
    card, _ = load_card_by_id(args.id)
    if not card:
        raise SystemExit(f"Card {args.id} not found.")
    now = get_now_formatted()
    card.update(status="Ajuste necessário", updated_at=now)
    if args.reason:
        note = f"[{now}] Motivo de reabertura: {args.reason}"
        card["notes_and_issues"] = f"{card.get('notes_and_issues')}\n{note}" if card.get("notes_and_issues") else note
    save_card(card)


def audit_card(card, apply_status=True):
    from card_audit import audit_contract, format_audit_report

    now = get_now_formatted()
    result = audit_contract(card.get("audit_contract"))
    card["audit_report"] = format_audit_report(result, now)
    if apply_status and result["configured"]:
        if result["passed"]:
            card.update(
                status="Implementado",
                completed_at=now,
                completion_summary="Automatic deterministic audit passed. See Audit Report for repository and validation evidence.",
            )
        elif card.get("status") == "Implementado":
            card["status"] = "Ajuste necessário"
        else:
            card["status"] = "Pendente"
    card["updated_at"] = now
    return result


def audit_and_save_card(card, apply_status=True):
    result = audit_card(card, apply_status=apply_status)
    save_card(card)
    return result


def cmd_audit(args):
    cards = [load_card_by_id(args.id)[0]] if args.id else load_all_cards()
    cards = [card for card in cards if card]
    for card in cards:
        result = audit_card(card, apply_status=not args.dry_run)
        if not args.dry_run:
            save_card(card)
        outcome = "PASS" if result["passed"] else "NOT CONFIGURED" if not result["configured"] else "FAIL"
        print(f"{card['id']}: {outcome} | {card['status']}")


def cmd_delete(args):
    card, card_file = load_card_by_id(args.id)
    if not card:
        raise SystemExit(f"Card {args.id} not found.")
    card_file.unlink()
    print(f"Card {card['id']} deleted.")


def cmd_migrate_json(args):
    migration = migrate_json_cards(args.apply)
    if not migration:
        print("No JSON Cards found.")
        return
    for source, destination, _ in migration:
        print(f"{source.name} -> {destination.name}")
    print("Migration applied." if args.apply else "Dry run only. Repeat with --apply to migrate and remove the JSON sources.")


def cmd_serve(args):
    from server import run_server
    run_server(port=args.port, open_browser=not args.no_browser)


def main():
    parser = argparse.ArgumentParser(description="Task Board CLI")
    subparsers = parser.add_subparsers(dest="command", required=True)
    for name, handler in (("list", cmd_list), ("search", cmd_search), ("show", cmd_show)):
        command = subparsers.add_parser(name)
        if name == "list":
            command.add_argument("--status"); command.add_argument("--type"); command.add_argument("--priority")
        elif name == "search":
            command.add_argument("query")
        else:
            command.add_argument("id")
        command.add_argument("--json", action="store_true")
        command.set_defaults(handler=handler)
    create = subparsers.add_parser("create")
    create.add_argument("--title", required=True); create.add_argument("--type", choices=VALID_TYPES, required=True)
    create.add_argument("--priority", choices=VALID_PRIORITIES, default="Média"); create.add_argument("--status", choices=VALID_STATUSES, default="Pendente")
    create.add_argument("--summary"); create.add_argument("--description"); create.add_argument("--prompt"); create.add_argument("--expected"); create.add_argument("--areas"); create.add_argument("--completion-summary")
    create.set_defaults(handler=cmd_create)
    for name, handler, argument in (("status", cmd_status, "status"), ("complete", cmd_complete, "summary"), ("reopen", cmd_reopen, "reason"), ("delete", cmd_delete, None)):
        command = subparsers.add_parser(name); command.add_argument("id")
        if argument == "status": command.add_argument("status", choices=VALID_STATUSES); command.add_argument("--summary")
        elif argument == "summary": command.add_argument("--summary", required=True)
        elif argument == "reason": command.add_argument("--reason")
        command.set_defaults(handler=handler)
    audit = subparsers.add_parser("audit")
    audit.add_argument("id", nargs="?")
    audit.add_argument("--dry-run", action="store_true")
    audit.set_defaults(handler=cmd_audit)
    migrate = subparsers.add_parser("migrate-json"); migrate.add_argument("--apply", action="store_true"); migrate.set_defaults(handler=cmd_migrate_json)
    serve = subparsers.add_parser("serve"); serve.add_argument("--port", type=int, default=8090); serve.add_argument("--no-browser", action="store_true"); serve.set_defaults(handler=cmd_serve)
    args = parser.parse_args()
    args.handler(args)


if __name__ == "__main__":
    main()
