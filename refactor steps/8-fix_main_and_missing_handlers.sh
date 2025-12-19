#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"

echo "🧩 Final wiring: main.go + missing handlers"

################################
# 1. Fix main.go
################################
cat > "$ROOT/cmd/webolla/main.go" <<'EOF'
package main

import (
	"log"
	"net/http"

	"webolla/internal/config"
	"webolla/internal/handlers"
	"webolla/internal/ollama"
)

func main() {
	cfg := config.Load()

	client := ollama.NewClient(cfg)
	h := handlers.New(cfg, client)

	mux := http.NewServeMux()
	mux.HandleFunc("/", h.ServeHTML)
	mux.HandleFunc("/api/action", h.OllamaAction)
	mux.HandleFunc("/api/cancel", h.Cancel)
	mux.HandleFunc("/api/status", h.Status)
	mux.HandleFunc("/api/models", h.ListModels)

	log.Printf("🚀 WebOlla listening on :%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
EOF

echo "✅ main.go fixed"

################################
# 2. Add Status handler
################################
cat > "$ROOT/internal/handlers/status.go" <<'EOF'
package handlers

import (
	"encoding/json"
	"net/http"
)

func (h *Handlers) Status(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]any{
		"ollamaBaseURL": h.cfg.OllamaBaseURL,
	})
}
EOF

echo "✅ Status handler added"

################################
# 3. Add ListModels handler (thin wrapper)
################################
cat > "$ROOT/internal/handlers/models.go" <<'EOF'
package handlers

import (
	"net/http"
)

func (h *Handlers) ListModels(w http.ResponseWriter, r *http.Request) {
	h.client.ListModels(w)
}
EOF

echo "✅ ListModels handler added"

################################
# 4. Build check
################################
echo ""
echo "🔍 Running go build ./..."
cd "$ROOT"
go build ./...

echo ""
echo "🎉 Build successful. Migration complete."
