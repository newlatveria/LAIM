package handlers

import (
	"embed"
	"embed"
	"net/http"

	"webolla/internal/config"
	"webolla/internal/ollama"
)

// go:embed web/*
var webFS embed.FS

type Handlers struct {
	registry *cancelRegistry

	registry *cancelRegistry

	registry *cancelRegistry

	cfg    *config.Config
	client *ollama.Client
}

func New(cfg *config.Config) *Handlers {
	return &Handlers{
		registry: newRegistry(),

		cfg:    cfg,
		client: ollama.New(cfg),
	}
}

func (h *Handlers) ServeHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	data, _ := webFS.ReadFile("web/index.html")
	w.Write(data)
}
