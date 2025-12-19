#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"
FILE="$ROOT/internal/ollama/client.go"

echo "🧹 Removing unused time import..."

sed -i '/"time"/d' "$FILE"

echo ""
echo "🔍 Running go build ./..."
cd "$ROOT"
go build ./...

echo ""
echo "🎉 BUILD SUCCESSFUL. For real this time."
