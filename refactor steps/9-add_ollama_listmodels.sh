#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"
OLLAMA="$ROOT/internal/ollama"

echo "🧩 Adding missing Ollama ListModels client method..."

################################
# 1. Add list_models.go
################################
cat > "$OLLAMA/list_models.go" <<'EOF'
package ollama

import (
	"encoding/json"
	"net/http"
)

func (c *Client) ListModels(w http.ResponseWriter) {
	resp, err := c.HTTP.Get(c.Cfg.TagsURL)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	json.NewDecoder(resp.Body).Decode(w)
}
EOF

echo "✅ ListModels added to ollama client"

################################
# 2. Build check
################################
echo ""
echo "🔍 Running go build ./..."
cd "$ROOT"
go build ./...

echo ""
echo "🎉 Build successful. You are DONE."
