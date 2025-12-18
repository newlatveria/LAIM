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
