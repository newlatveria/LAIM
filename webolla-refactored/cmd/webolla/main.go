package main

import (
	"log"
	"net/http"

	"webolla/internal/config"
	"webolla/internal/handlers"
)

func main() {
	cfg := config.Load()
	h := handlers.New(cfg)

	http.HandleFunc("/", h.ServeHTML)
	http.HandleFunc("/api/ollama-action", h.OllamaAction)
	http.HandleFunc("/api/models", h.ListModels)
	http.HandleFunc("/api/status", h.Status)
	http.HandleFunc("/api/cancel", h.Cancel)

	log.Printf("Server listening on :%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}
