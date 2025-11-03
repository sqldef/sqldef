#!/bin/bash
set -xeu -o pipefail

cd "$CLAUDE_PROJECT_DIR"

PATH_CHANGED=$(jq -r '.tool_input.path // empty' 2>/dev/null)

if [[ "$PATH_CHANGED" =~ ^.*\.(go|y)$ ]]; then
    make test || exit 2
fi

exit 0
