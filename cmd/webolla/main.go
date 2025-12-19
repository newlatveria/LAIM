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
