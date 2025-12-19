#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"
H="$ROOT/internal/handlers"

echo "🧯 Repairing handlers package..."

################################
# 1. Fix handlers.go completely
################################
cat > "$H/handlers.go" <<'EOF'
package handlers

import (
	"embed"
	"net/http"

	"webolla/internal/config"
	"webolla/internal/ollama"
)

//go:embed web/*
var webFS embed.FS

type Handlers struct {
	cfg      *config.Config
	client   *ollama.Client
	registry *cancelRegistry
}

func New(cfg *config.Config, client *ollama.Client) *Handlers {
	return &Handlers{
		cfg:      cfg,
		client:   client,
		registry: newRegistry(),
	}
}

func (h *Handlers) ServeHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	data, _ := webFS.ReadFile("web/index.html")
	w.Write(data)
}
EOF

echo "✅ handlers.go rebuilt cleanly"

################################
# 2. Fix ollama_action.go
################################
cat > "$H/ollama_action.go" <<'EOF'
package handlers

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"webolla/internal/ollama"
)

type ClientRequest struct {
	ActionType string                  `json:"actionType"`
	Model      string                  `json:"model"`
	Prompt     string                  `json:"prompt"`
	Messages   []ollama.Message        `json:"messages"`
	Params     ollama.GenerationParams `json:"params"`
}

func (h *Handlers) OllamaAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = uuid.New().String()
		w.Header().Set("X-Request-ID", requestID)
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.GenerateTimeout)
	h.registry.Add(requestID, cancel)
	defer h.registry.Remove(requestID)

	switch req.ActionType {
	case "generate":
		_ = h.client.GenerateStream(ctx, w, req.Model, req.Prompt, req.Params)
	case "chat":
		_ = h.client.ChatStream(ctx, w, req.Model, req.Messages, req.Params)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}
EOF

echo "✅ ollama_action.go rebuilt cleanly"

################################
# 3. Final build check
################################
echo ""
echo "🔍 Running go build ./..."
cd "$ROOT"
go build ./...

echo ""
echo "🎉 handlers package repaired and build is clean"
