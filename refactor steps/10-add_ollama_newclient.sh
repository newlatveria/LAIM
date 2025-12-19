#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"
OLLAMA="$ROOT/internal/ollama"

echo "🔩 Adding Ollama NewClient constructor..."

cat > "$OLLAMA/client.go" <<'EOF'
package ollama

import (
	"net/http"
	"time"

	"webolla/internal/config"
)

type Client struct {
	HTTP *http.Client
	Cfg  *config.Config
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		Cfg: cfg,
		HTTP: &http.Client{
			Timeout: 0, // streaming requests manage timeouts via context
		},
	}
}
EOF

echo "✅ NewClient added"

echo ""
echo "🔍 Running go build ./..."
cd "$ROOT"
go build ./...

echo ""
echo "🎉 BUILD SUCCESSFUL. This really is the end."
