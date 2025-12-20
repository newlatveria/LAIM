import os

project_files = {
    "webolla-fixed/go.mod": "module webolla\n\ngo 1.22",
    
    # NEW: embed.go at the root solves the 'pattern not found' issue
    "webolla-fixed/embed.go": """package webolla
import "embed"

//go:embed web/*
var WebContent embed.FS
""",

    "webolla-fixed/cmd/webolla/main.go": """package main
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
	webFS, err := fs.Sub(webolla.WebContent, "web")
	if err != nil { log.Fatal(err) }

	h := handlers.New(cfg, webFS)
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.Index)
	mux.HandleFunc("/api/models", h.Models)
	mux.HandleFunc("/api/upload", h.Upload)

	log.Printf("Listening on :%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
""",

    "webolla-fixed/internal/handlers/handlers.go": """package handlers
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
		client: &http.Client{Timeout: 5 * time.Second},
	}
}
func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
    // FIX: Explicitly serve index.html for the root path to avoid 404
	if r.URL.Path == "/" {
		f, err := h.webFS.Open("index.html")
		if err != nil {
			http.Error(w, "Not Found", 404)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.Copy(w, f)
		return
	}
	http.FileServer(http.FS(h.webFS)).ServeHTTP(w, r)
}
func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, err := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	if err != nil {
		http.Error(w, "Failed to connect to Ollama", 502)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}
func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 500<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "Upload error", 400)
		return
	}
	files := r.MultipartForm.File["files"]
	os.MkdirAll(h.cfg.UploadDir, 0755)
	successCount := 0
	for _, f := range files {
		err := func() error {
			src, err := f.Open()
			if err != nil { return err }
			defer src.Close()
			dstPath := filepath.Join(h.cfg.UploadDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(f.Filename)))
			dst, err := os.Create(dstPath)
			if err != nil { return err }
			defer dst.Close()
			_, err = io.Copy(dst, src)
			return err
		}()
		if err == nil { successCount++ }
	}
	json.NewEncoder(w).Encode(map[string]any{"uploaded": successCount})
}
""",
    "webolla-fixed/internal/config/config.go": """package config
import "os"
type Config struct {
	Port, OllamaBaseURL, UploadDir string
}
func Load() *Config {
	return &Config{
		Port: env("PORT", "8080"),
		OllamaBaseURL: env("OLLAMA_BASE_URL", "http://127.0.0.1:11434"),
		UploadDir: env("UPLOAD_DIR", "uploads"),
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" { return v }
	return d
}
""",

    "webolla-fixed/web/index.html": """<!doctype html>
<html>
<head>
  <meta charset="utf-8"/>
  <title>WebOlla (Secure)</title>
  <style>
    body { font-family: sans-serif; max-width: 800px; margin: 2rem auto; padding: 0 1rem; }
    .status { margin-top: 1rem; padding: 1rem; border-radius: 4px; display: none; }
    .success { background: #d4edda; color: #155724; }
    .error { background: #f8d7da; color: #721c24; }
  </style>
</head>
<body>
<h1>WebOlla (Secure)</h1>

<h2>Available Models</h2>
<ul id="models"><li>Loading...</li></ul>

<h2>Upload Files or Folders</h2>
<input type="file" id="files" multiple>
<input type="file" id="folders" webkitdirectory multiple>
<br><br>
<button onclick="upload()">Upload</button>

<div id="status" class="status"></div>

<script>
async function loadModels() {
  const list = document.getElementById("models");
  try {
    const res = await fetch("/api/models");
    if (!res.ok) throw new Error("Server error");
    const data = await res.json();
    list.innerHTML = "";
    if (!data.models || data.models.length === 0) {
        list.innerHTML = "<li>No models found</li>";
        return;
    }
    for (const m of data.models) {
      const li = document.createElement("li");
      li.textContent = m.name;
      list.appendChild(li);
    }
  } catch (e) {
    list.innerHTML = "<li>Error loading models. Is Ollama running?</li>";
  }
}

async function upload() {
  const statusDiv = document.getElementById("status");
  statusDiv.style.display = "block";
  statusDiv.className = "status";
  statusDiv.textContent = "Uploading...";

  const data = new FormData();
  const f1 = document.getElementById("files").files;
  const f2 = document.getElementById("folders").files;
  
  if (f1.length === 0 && f2.length === 0) {
    statusDiv.className = "status error";
    statusDiv.textContent = "Please select files first.";
    return;
  }

  for (const f of f1) data.append("files", f);
  for (const f of f2) data.append("files", f);

  try {
    const res = await fetch("/api/upload", {
      method: "POST",
      body: data
    });

    if (!res.ok) {
        const text = await res.text();
        throw new Error(text || "Upload failed");
    }

    const result = await res.json();
    statusDiv.className = "status success";
    statusDiv.textContent = `Success: ${result.uploaded} files uploaded.`;
  } catch (e) {
    statusDiv.className = "status error";
    statusDiv.textContent = "Error: " + e.message;
  }
}

loadModels();
</script>
</body>
</html>
"""
}

def create_project():
    print("Creating project files...")
    for filepath, content in project_files.items():
        # Create directories if they don't exist
        os.makedirs(os.path.dirname(filepath), exist_ok=True)
        
        # Write the file content
        with open(filepath, "w", encoding="utf-8") as f:
            f.write(content)
        print(f"Created: {filepath}")
    
    print("\\nSuccess! The 'webolla-fixed' folder has been created.")
    print("Run the project with:")
    print("  cd webolla-fixed")
    print("  go run cmd/webolla/main.go")

if __name__ == "__main__":
    create_project()
