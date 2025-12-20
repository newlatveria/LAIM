package main

import (
	"io/fs"
	"log"
	"net/http"
	"webolla" 
	"webolla/internal/config"
	"webolla/internal/handlers"
)

func main() {
	cfg := config.Load()

	// Sub-folder 'web' so that index.html is at the root of the filesystem
	webFS, err := fs.Sub(webolla.WebContent, "web")
	if err != nil {
		log.Fatal(err)
	}

	h := handlers.New(cfg, webFS)

	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Index)
	mux.HandleFunc("/api/models", h.Models)
	mux.HandleFunc("/api/upload", h.Upload)

	log.Printf("Server starting on http://localhost:%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
