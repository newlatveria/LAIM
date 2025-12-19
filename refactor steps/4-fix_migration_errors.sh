#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"

echo "🧹 Fixing migration artifacts..."

############################
# 1. Remove raw reference files
############################
RAW_FILES=(
  "$ROOT/internal/config/config_raw.go"
  "$ROOT/internal/handlers/handlers_raw.go"
)

for f in "${RAW_FILES[@]}"; do
  if [[ -f "$f" ]]; then
    echo "🗑️  Removing $f"
    rm "$f"
  fi
done

############################
# 2. Ensure package declaration in ollama/types.go
############################
OLLAMA_TYPES="$ROOT/internal/ollama/types.go"

if [[ -f "$OLLAMA_TYPES" ]]; then
  FIRST_LINE=$(head -n 1 "$OLLAMA_TYPES")
  if [[ "$FIRST_LINE" != package* ]]; then
    echo "🩹 Fixing package declaration in ollama/types.go"
    sed -i '1s/^/package ollama\n\n/' "$OLLAMA_TYPES"
  fi
fi

############################
# 3. Fix handlers.go header if damaged
############################
HANDLERS_FILE="$ROOT/internal/handlers/handlers.go"

if [[ -f "$HANDLERS_FILE" ]]; then
  if ! grep -q '^package handlers' "$HANDLERS_FILE"; then
    echo "🩹 Restoring package declaration in handlers.go"

    sed -i '1s/^/package handlers\n\n/' "$HANDLERS_FILE"
  fi
fi

############################
# 4. Ensure registry field exists
############################
if ! grep -q 'registry \*cancelRegistry' "$HANDLERS_FILE"; then
  echo "🩹 Adding registry field to Handlers struct"

  sed -i '/type Handlers struct {/a\
\tregistry *cancelRegistry\
' "$HANDLERS_FILE"
fi

############################
# 5. Final build check
############################
echo ""
echo "🔍 Running go build ./..."
cd "$ROOT"
go build ./...

echo ""
echo "✅ Migration cleanup complete"
