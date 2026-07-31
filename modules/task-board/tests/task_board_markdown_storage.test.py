import json
import sys
import tempfile
from pathlib import Path

SCRIPT_DIR = Path(__file__).resolve().parents[1] / "scripts"
sys.path.insert(0, str(SCRIPT_DIR))
import task_board


def make_card(card_id="TASK-0042"):
    return {
        "id": card_id,
        "title": "Preserve Markdown body",
        "summary": "Keep one Markdown Card as the canonical source.",
        "description": "A requirement with a real newline.\n\n- Preserve this list.",
        "ai_prompt": "# Tarefa\n\n## Regras\n\nDo not lose headings or code paths.",
        "expected_features": "- One canonical file\n- Stable metadata",
        "audit_contract": "- source: task-board/scripts/task_board.py contains save_card\n- validation: task-board-card-importer",
        "audit_report": "Last audit: pending",
        "impacted_areas": ["Task Board", "Storage"],
        "type": "Melhoria",
        "status": "Pendente",
        "priority": "Alta",
        "created_at": "01/01/2026 10:00 (America/Sao_Paulo)",
        "updated_at": "01/01/2026 10:00 (America/Sao_Paulo)",
        "completed_at": None,
        "completion_summary": None,
        "notes_and_issues": None,
    }


with tempfile.TemporaryDirectory() as temporary_directory:
    original_cards_dir = task_board.CARDS_DIR
    task_board.CARDS_DIR = Path(temporary_directory)
    try:
        card = make_card()
        markdown_file = task_board.save_card(card)
        assert markdown_file.suffix == ".md"
        assert not (task_board.CARDS_DIR / "TASK-0042.json").exists()
        loaded_card = task_board.read_card_file(markdown_file)
        metrics = loaded_card.pop("markdown_token_metrics")
        assert metrics["estimated"] is True
        assert metrics["lines"] > 1
        assert metrics["profiles"]["codex"]["tokens"] > 0
        assert loaded_card == card

        legacy_card = make_card("TASK-0043")
        legacy_file = task_board.CARDS_DIR / "TASK-0043.json"
        legacy_file.write_text(json.dumps(legacy_card, ensure_ascii=False), encoding="utf-8")
        preview = task_board.migrate_json_cards(apply=False)
        assert [source.name for source, _, _ in preview] == ["TASK-0043.json"]
        task_board.migrate_json_cards(apply=True)
        assert not legacy_file.exists()
        loaded_legacy_card = task_board.read_card_file(task_board.CARDS_DIR / "TASK-0043.md")
        loaded_legacy_card.pop("markdown_token_metrics")
        assert loaded_legacy_card == legacy_card
    finally:
        task_board.CARDS_DIR = original_cards_dir

print("task_board_markdown_storage.test.py: OK")
