package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"webolla/internal/config"
)

type Handlers struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Handlers {
	return &Handlers{cfg: cfg}
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "web/index.html")
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(h.cfg.OllamaBaseURL + "/api/tags")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "no files uploaded", 400)
		return
	}

	os.MkdirAll("uploads", 0755)

	for _, f := range files {
		src, err := f.Open()
		if err != nil {
			continue
		}
		defer src.Close()

		dstPath := filepath.Join("uploads", filepath.Base(f.Filename))
		dst, err := os.Create(dstPath)
		if err != nil {
			continue
		}
		io.Copy(dst, src)
		dst.Close()
	}

	json.NewEncoder(w).Encode(map[string]any{
		"uploaded": len(files),
	})
}
