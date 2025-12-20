import os

project_files = {
    # 1. Project Root - go.mod
    "webolla-fixed/go.mod": "module webolla\n\ngo 1.22",
    
    # 2. Project Root - embed.go (Fixes the "pattern web: no matching files" error)
    "webolla-fixed/embed.go": """package webolla
import "embed"

//go:embed web/*
var WebContent embed.FS
""",

    # 3. Main Entry Point
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
""",

    # 4. Handlers (Fixes the 404 error and resource leaks)
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
""",

    # 5. Config
    "webolla-fixed/internal/config/config.go": """package config
import "os"
type Config struct {
	Port, OllamaBaseURL, UploadDir string
}
func Load() *Config {
	return &Config{
		Port:          env("PORT", "8080"),
		OllamaBaseURL: env("OLLAMA_BASE_URL", "http://127.0.0.1:11434"),
		UploadDir:     env("UPLOAD_DIR", "uploads"),
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" { return v }
	return d
}
""",

    # 6. Frontend
    "webolla-fixed/web/index.html": """<!doctype html>
<html>
<head>
  <meta charset="utf-8"/>
  <title>WebOlla (Secure)</title>
  <style>
    body { font-family: sans-serif; max-width: 800px; margin: 2rem auto; padding: 0 1rem; line-height: 1.6; }
    .status { margin-top: 1rem; padding: 1rem; border-radius: 4px; display: none; border: 1px solid #ccc; }
    .success { background: #d4edda; color: #155724; border-color: #c3e6cb; }
    .error { background: #f8d7da; color: #721c24; border-color: #f5c6cb; }
    ul { background: #f9f9f9; padding: 1rem 2rem; border-radius: 8px; }
  </style>
</head>
<body>
<h1>WebOlla Control</h1>

<h3>Ollama Models</h3>
<ul id="models"><li>Checking connection...</li></ul>

<h3>File Management</h3>
<div style="border: 1px dashed #aaa; padding: 20px; border-radius: 8px;">
    <label>Select Files:</label><br>
    <input type="file" id="files" multiple><br><br>
    <label>Select Folders:</label><br>
    <input type="file" id="folders" webkitdirectory multiple><br><br>
    <button onclick="upload()" style="padding: 10px 20px; cursor: pointer;">Start Upload</button>
</div>

<div id="status" class="status"></div>

<script>
async function loadModels() {
  const list = document.getElementById("models");
  try {
    const res = await fetch("/api/models");
    if (!res.ok) throw new Error("Could not reach Ollama");
    const data = await res.json();
    list.innerHTML = data.models?.length 
        ? data.models.map(m => `<li><b>${m.name}</b> (${(m.size/1e9).toFixed(2)} GB)</li>`).join('')
        : "<li>No models currently loaded in Ollama.</li>";
  } catch (e) {
    list.innerHTML = `<li style="color:red">Error: ${e.message}. Is Ollama running on port 11434?</li>`;
  }
}

async function upload() {
  const statusDiv = document.getElementById("status");
  statusDiv.style.display = "block";
  statusDiv.className = "status";
  statusDiv.textContent = "Uploading assets...";

  const data = new FormData();
  const f1 = document.getElementById("files").files;
  const f2 = document.getElementById("folders").files;
  
  if (f1.length === 0 && f2.length === 0) {
    statusDiv.className = "status error";
    statusDiv.textContent = "Please select at least one file or folder.";
    return;
  }

  for (const f of f1) data.append("files", f);
  for (const f of f2) data.append("files", f);

  try {
    const res = await fetch("/api/upload", { method: "POST", body: data });
    if (!res.ok) throw new Error(await res.text());
    const result = await res.json();
    statusDiv.className = "status success";
    statusDiv.textContent = `Successfully uploaded ${result.uploaded} files.`;
  } catch (e) {
    statusDiv.className = "status error";
    statusDiv.textContent = "Upload failed: " + e.message;
  }
}

loadModels();
</script>
</body>
</html>
"""
}

def build():
    for path, content in project_files.items():
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f:
            f.write(content)
        print(f"Created {path}")
    print("\nProject built successfully in 'webolla-fixed/'")

if __name__ == "__main__":
    build()