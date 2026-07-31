#!/usr/bin/env bash
set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
cd "$REPO_ROOT"

PYTHON_BIN="${PYTHON_BIN:-python3}"
if ! command -v "$PYTHON_BIN" >/dev/null 2>&1; then
  echo "Python 3 was not found. Install Python 3, then run this launcher again."
  read -r -p "Press Return to close..."
  exit 1
fi

"$PYTHON_BIN" "Code Registry/tools/code_registry.py" full
if [ $? -ne 0 ]; then
  echo
  echo "Code Registry found items that need review. The Explorer will still open."
fi

exec "$PYTHON_BIN" "Code Registry/tools/code_registry.py" serve
