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
	mux := http.NewServeMux()
	
	// API routes first (more specific)
	mux.HandleFunc("/api/models", h.Models)
	mux.HandleFunc("/api/generate", h.Generate)
	mux.HandleFunc("/api/rag", h.Rag)
	mux.HandleFunc("/api/upload", h.Upload)
	mux.HandleFunc("/api/reindex", h.ReindexAll)
	mux.HandleFunc("/api/telemetry", h.Telemetry)
	
	// Catch-all for index (must be last)
	mux.HandleFunc("/", h.Index)
	
	log.Printf("WebOlla running at http://localhost:%s\n", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}