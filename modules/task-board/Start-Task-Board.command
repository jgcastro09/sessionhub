#!/bin/bash
DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$DIR/.."
echo "=================================================="
echo "  Iniciando NodeStage Task Board Server..."
echo "  ATENCAO: Mantenha este terminal ABERTO enquanto usar!"
echo "  Se fechar esta janela, o servidor sera desligado."
echo "=================================================="
echo ""
python3 "task-board/scripts/task_board.py" serve
