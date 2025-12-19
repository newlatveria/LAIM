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
