@echo off
setlocal

set "SCRIPT_DIR=%~dp0"
for %%I in ("%SCRIPT_DIR%..\..") do set "REPO_ROOT=%%~fI"
cd /d "%REPO_ROOT%"

set "PYTHON=python"
where py >nul 2>nul && set "PYTHON=py -3"

%PYTHON% "Code Registry\tools\code_registry.py" full
if errorlevel 1 (
    echo.
    echo Code Registry found items that need review. The Explorer will still open.
)

%PYTHON% "Code Registry\tools\code_registry.py" serve
pause
