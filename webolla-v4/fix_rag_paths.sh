#!/usr/bin/env bash
set -euo pipefail

echo "🧱 Fixing RAG + upload paths (no more relative path bugs)"

ROOT="$(pwd)"
DATA_DIR="$ROOT/data"
UPLOADS_DIR="$DATA_DIR/uploads"
RAG_DIR="$DATA_DIR/rag"

mkdir -p "$UPLOADS_DIR" "$RAG_DIR"

HANDLERS="internal/handlers/handlers.go"
RAG="internal/handlers/rag.go"

for f in "$HANDLERS" "$RAG"; do
  if [[ ! -f "$f" ]]; then
    echo "❌ Missing file: $f"
    exit 1
  fi
done

echo "📦 Backing up files"
cp "$HANDLERS" "$HANDLERS.bak"
cp "$RAG" "$RAG.bak"

echo "🛠 Patching handlers.go"

sed -i '
s|os.MkdirAll("uploads"|os.MkdirAll("data/uploads"|;
s|os.MkdirAll("rag"|os.MkdirAll("data/rag"|;
s|filepath.Join("uploads"|filepath.Join("data/uploads"|;
' "$HANDLERS"

echo "🛠 Patching rag.go"

sed -i '
s|"rag/index.json"|"data/rag/index.json"|g
' "$RAG"

echo "✅ Paths fixed"
echo
echo "📂 Uploads → data/uploads"
echo "📂 RAG index → data/rag/index.json"
echo
echo "▶ Restart with:"
echo "   go run ./cmd/webolla"
