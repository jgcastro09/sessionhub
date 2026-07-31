@echo off
title NodeStage Task Board Server
setlocal

set "SCRIPT_DIR=%~dp0"
cd /d "%SCRIPT_DIR%.."

set "PYTHON=python"
where py >nul 2>nul && set "PYTHON=py -3"

echo ==================================================
echo   Iniciando NodeStage Task Board Server...
echo   ATENCAO: Mantenha esta janela ABERTA enquanto usar!
echo   Se fechar esta janela, o servidor sera desligado.
echo ==================================================
echo.

%PYTHON% "task-board\scripts\task_board.py" serve

echo.
echo Servidor encerrado.
pause
