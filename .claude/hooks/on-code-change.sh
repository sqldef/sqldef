#!/bin/bash
set -xeu -o pipefail

cd "$CLAUDE_PROJECT_DIR"

changed_go_files=$(git diff --name-only HEAD | grep '\.go$')
if [ -n "$changed_go_files" ]; then
  make test || exit 2
fi
