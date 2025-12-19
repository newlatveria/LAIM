#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"
OLLAMA="$ROOT/internal/ollama"

echo "🛠️  Fixing Ollama build errors..."

############################
# 1. Add buildOptions helper
############################
cat > "$OLLAMA/options.go" <<'EOF'
package ollama

func buildOptions(params GenerationParams) map[string]interface{} {
	opts := make(map[string]interface{})

	if params.Temperature > 0 {
		opts["temperature"] = params.Temperature
	}
	if params.TopP > 0 {
		opts["top_p"] = params.TopP
	}
	if params.TopK > 0 {
		opts["top_k"] = params.TopK
	}
	if params.RepeatPenalty > 0 {
		opts["repeat_penalty"] = params.RepeatPenalty
	}
	if params.NumPredict > 0 {
		opts["num_predict"] = params.NumPredict
	}

	return opts
}
EOF

echo "✅ Added internal/ollama/options.go"

############################
# 2. Remove unused io import
############################
STREAM_FILE="$OLLAMA/stream.go"

if grep -q '"io"' "$STREAM_FILE"; then
  echo "🧹 Removing unused io import from stream.go"
  sed -i '/"io"/d' "$STREAM_FILE"
fi

############################
# 3. Build check
############################
echo ""
echo "🔍 Running go build ./..."
cd "$ROOT"
go build ./...

echo ""
echo "✅ Ollama build errors fixed"
