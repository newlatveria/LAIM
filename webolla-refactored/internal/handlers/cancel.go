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
