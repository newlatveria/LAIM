package handlers
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
	"webolla/internal/config"
)
type Handlers struct {
	cfg    *config.Config
	client *http.Client
}
func New(cfg *config.Config) *Handlers {
	return &Handlers{
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}
func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join("web", "index.html"))
}
func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, err := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	if err != nil {
		http.Error(w, "Ollama unreachable", 502); return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}
func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]any
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid JSON", 400); return
	}
	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		http.Error(w, "Ollama Error", 500); return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}
func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 500<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "Upload failed", 400); return
	}
	files := r.MultipartForm.File["files"]
	os.MkdirAll(h.cfg.UploadDir, 0755)
	for _, f := range files {
		func() {
			src, err := f.Open()
			if err != nil { return }
			defer src.Close()
			// FIX: Using fmt.Sprintf here keeps the import valid
			fname := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(f.Filename))
			dst, err := os.Create(filepath.Join(h.cfg.UploadDir, fname))
			if err != nil { return }
			defer dst.Close()
			io.Copy(dst, src)
		}()
	}
	json.NewEncoder(w).Encode(map[string]any{"uploaded": len(files)})
}
