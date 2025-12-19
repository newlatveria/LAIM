#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"

echo "🛑 Wiring real request cancellation..."

############################
# 1. Request registry
############################
cat > $ROOT/internal/handlers/registry.go <<'EOF'
package handlers

import "sync"

type cancelRegistry struct {
	mu sync.Mutex
	m  map[string]func()
}

func newRegistry() *cancelRegistry {
	return &cancelRegistry{m: make(map[string]func())}
}

func (r *cancelRegistry) Add(id string, cancel func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[id] = cancel
}

func (r *cancelRegistry) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if cancel, ok := r.m[id]; ok {
		cancel()
		delete(r.m, id)
		return true
	}
	return false
}

func (r *cancelRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, id)
}
EOF

############################
# 2. Extend Handlers
############################
sed -i -e '/type Handlers struct {/a\
	registry *cancelRegistry\
' $ROOT/internal/handlers/handlers.go

sed -i -e '/return &Handlers{/a\
		registry: newRegistry(),\
' $ROOT/internal/handlers/handlers.go


############################
# 3. Rewrite OllamaAction with cancellation
############################
cat > $ROOT/internal/handlers/ollama_action.go <<'EOF'
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type ClientRequest struct {
	ActionType string           `json:"actionType"`
	Model      string           `json:"model"`
	Prompt     string           `json:"prompt"`
	Messages   []Message        `json:"messages"`
	Params     GenerationParams `json:"params"`
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

############################
# 4. Implement real /api/cancel
############################
cat > $ROOT/internal/handlers/cancel.go <<'EOF'
package handlers

import (
	"encoding/json"
	"net/http"
)

func (h *Handlers) Cancel(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("requestId")
	if id == "" {
		http.Error(w, "missing requestId", http.StatusBadRequest)
		return
	}

	ok := h.registry.Cancel(id)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"cancelled": ok,
	})
}
EOF

############################
# 5. Dependency note
############################
echo ""
echo "📦 Adding dependency: github.com/google/uuid"
echo "Run:"
echo "  cd webolla-refactored"
echo "  go get github.com/google/uuid"

echo ""
echo "✅ Request cancellation wired"
