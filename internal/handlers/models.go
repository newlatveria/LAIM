package handlers

import (
	"net/http"
)

func (h *Handlers) ListModels(w http.ResponseWriter, r *http.Request) {
	h.client.ListModels(w)
}
