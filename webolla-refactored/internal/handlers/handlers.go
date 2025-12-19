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
