import sys
from pathlib import Path


SCRIPTS_DIR = Path(__file__).resolve().parents[1] / "scripts"
sys.path.insert(0, str(SCRIPTS_DIR))

from card_audit import audit_contract, format_audit_report
import task_board


passing = audit_contract("""
- source: task-board/scripts/card_audit.py contains VALIDATION_COMMANDS
- validation: task-board-card-importer
""")
assert passing["configured"]
assert passing["passed"], passing
assert "Result: PASS" in format_audit_report(passing, "01/01/2026 00:00 (America/Sao_Paulo)")

failing = audit_contract("""
- source: task-board/scripts/card_audit.py contains definitely_missing_literal
- validation: task-board-card-importer
""")
assert failing["configured"]
assert not failing["passed"]

not_configured = audit_contract("")
assert not not_configured["configured"]
assert not not_configured["passed"]

card = {
    "id": "TASK-TEST",
    "status": "Pendente",
    "audit_contract": "- source: task-board/scripts/card_audit.py contains VALIDATION_COMMANDS\n- validation: task-board-card-importer",
}
result = task_board.audit_card(card)
assert result["passed"]
assert card["status"] == "Implementado"
assert "Result: PASS" in card["audit_report"]

regression = {
    "id": "TASK-TEST",
    "status": "Implementado",
    "audit_contract": "- source: task-board/scripts/card_audit.py contains missing_literal\n- validation: task-board-card-importer",
}
task_board.audit_card(regression)
assert regression["status"] == "Ajuste necessário"

print("card_audit.test.py: OK")
