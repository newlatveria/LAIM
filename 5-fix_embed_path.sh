#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"
FILE="$ROOT/internal/handlers/handlers.go"

echo "🧩 Fixing go:embed path issue..."

############################
# 1. Replace invalid embed
############################
sed -i \
  -e 's#//go:embed .*#//go:embed web/*#' \
  "$FILE"

############################
# 2. Fix ReadFile path
############################
sed -i \
  -e 's#ReadFile(".*")#ReadFile("web/index.html")#' \
  "$FILE"

############################
# 3. Move embed to module root context
############################
# go:embed paths are relative to the package directory,
# so we must import from module root using an alias

sed -i \
  -e 's/import (/import (\n\t"embed"/' \
  "$FILE"

############################
# 4. Build check
############################
echo ""
echo "🔍 Running go build ./..."
cd "$ROOT"
go build ./...

echo ""
echo "✅ go:embed fixed and build successful"
