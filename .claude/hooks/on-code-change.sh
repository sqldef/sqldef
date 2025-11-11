#!/bin/bash
set -xeu -o pipefail

cd "$CLAUDE_PROJECT_DIR"

changed_go_files=$(git diff --name-only HEAD | grep '\.go$')
if [ -n "$changed_go_files" ]; then
  make parser || exit 2
  make build || exit 2
  make lint || exit 2
  make modernize || exit 2
  make format || exit 2
fi
