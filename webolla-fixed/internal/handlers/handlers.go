package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"webolla/internal/config"
)

type Handlers struct {
	cfg    *config.Config
	webFS  fs.FS
	client *http.Client
}

func New(cfg *config.Config, webFS fs.FS) *Handlers {
	return &Handlers{
		cfg:   cfg,
		webFS: webFS,
		client: &http.Client{
			Timeout: 5 * time.Second, // Prevents hanging if Ollama is down
		},
	}
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	// FIX for 404: Explicitly serve index.html for the root path
	if r.URL.Path == "/" {
		f, err := h.webFS.Open("index.html")
		if err != nil {
			http.Error(w, "Index Not Found", 404)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.Copy(w, f)
		return
	}

	// Serve other static files (css/js) if they exist
	http.FileServer(http.FS(h.webFS)).ServeHTTP(w, r)
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, err := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	if err != nil {
		http.Error(w, "Ollama connection error", 502)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 500<<20) // Limit to 500MB
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "Request too large", 400)
		return
	}

	files := r.MultipartForm.File["files"]
	os.MkdirAll(h.cfg.UploadDir, 0755)

	count := 0
	for _, f := range files {
		err := func() error {
			src, err := f.Open()
			if err != nil { return err }
			defer src.Close()

			// Timestamp prefix prevents file overwrites
			fname := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(f.Filename))
			dst, err := os.Create(filepath.Join(h.cfg.UploadDir, fname))
			if err != nil { return err }
			defer dst.Close()

			_, err = io.Copy(dst, src)
			return err
		}()
		if err == nil { count++ }
	}

	json.NewEncoder(w).Encode(map[string]any{"uploaded": count})
}
