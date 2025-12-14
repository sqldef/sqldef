#!/bin/bash
set -xeu -o pipefail

cd "$CLAUDE_PROJECT_DIR"

if git diff --name-only HEAD | grep -q '\.go$'; then
  make parser || exit 2
  make build || exit 2
  make lint || exit 2
  make modernize || exit 2
  make format || exit 2
fi
