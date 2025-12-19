#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"
HANDLERS_DIR="$ROOT/internal/handlers"

echo "🧩 Final embed fix (simple & reliable)..."

############################
# 1. Ensure target directory
############################
mkdir -p "$HANDLERS_DIR/web"

############################
# 2. Copy web assets locally
############################
if [[ -d "$ROOT/web" ]]; then
  echo "📁 Copying web/ → internal/handlers/web/"
  cp -r "$ROOT/web/"* "$HANDLERS_DIR/web/"
else
  echo "❌ web/ directory not found"
  exit 1
fi

############################
# 3. Fix handlers.go embed directive
############################
FILE="$HANDLERS_DIR/handlers.go"

sed -i \
  -e 's#//go:embed .*#//go:embed web/*#' \
  "$FILE"

sed -i \
  -e 's#ReadFile(".*")#ReadFile("web/index.html")#' \
  "$FILE"

############################
# 4. Build check
############################
echo ""
echo "🔍 Running go build ./..."
cd "$ROOT"
go build ./...

echo ""
echo "✅ Embed fixed. Build successful."
