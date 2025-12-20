import os

project_files = {
    "webolla-v3/go.mod": "module webolla\n\ngo 1.22",
    
    "webolla-v3/cmd/webolla/main.go": """package main
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
    
	mux.HandleFunc("/", h.Index)
	mux.HandleFunc("/api/models", h.Models)
	mux.HandleFunc("/api/generate", h.Generate)
	mux.HandleFunc("/api/upload", h.Upload)

	log.Printf("Server starting on http://localhost:%s\\n", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
""",

    "webolla-v3/internal/config/config.go": """package config
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

    "webolla-v3/internal/handlers/handlers.go": """package handlers
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
    // Serving from relative path 'web/index.html'
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
	w.Header().Set("Content-Type", "application/x-ndjson") // Stream or copy result
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
			src, _ := f.Open(); defer src.Close()
			dst, _ := os.Create(filepath.Join(h.cfg.UploadDir, filepath.Base(f.Filename)))
			defer dst.Close()
			io.Copy(dst, src)
		}()
	}
	json.NewEncoder(w).Encode(map[string]any{"uploaded": len(files)})
}
""",

    "webolla-v3/web/index.html": """<!doctype html>
<html>
<head>
<meta charset="utf-8"/><title>WebOlla v3</title>
<style>
    body { font-family: sans-serif; max-width: 900px; margin: 2rem auto; padding: 0 1rem; line-height: 1.5; }
    textarea { width: 100%; padding: 10px; border-radius: 5px; border: 1px solid #ccc; font-family: monospace; }
    .box { border: 1px solid #ddd; padding: 20px; border-radius: 8px; background: #fdfdfd; margin-bottom: 20px; }
    pre { background: #333; color: #fff; padding: 15px; border-radius: 5px; white-space: pre-wrap; word-wrap: break-word; }
</style>
</head>
<body>
<h1>WebOlla v3</h1>

<div class="box">
    <h3>1. Setup Task</h3>
    <select id="task" onchange="recommend()" style="padding: 5px;">
        <option value="advanced">General / Advanced</option>
        <option value="code">Coding Specialist</option>
        <option value="fast">Fast / Tiny (3B or less)</option>
    </select>
    <select id="models" style="padding: 5px;"></select>
</div>

<div class="box">
    <h3>2. Generate</h3>
    <textarea id="prompt" rows="5" placeholder="What's on your mind?"></textarea><br><br>
    <button onclick="generate()" id="genBtn" style="padding: 10px 20px; background: #007bff; color: white; border: none; border-radius: 4px; cursor: pointer;">Generate Response</button>
    <h4>Output:</h4>
    <pre id="output">Results will appear here...</pre>
</div>

<div class="box">
    <h3>3. Knowledge Base Upload</h3>
    <input type="file" id="files" multiple>
    <button onclick="upload()">Upload Files</button>
    <p id="upStatus"></p>
</div>

<script>
let allModels = [];

async function loadModels() {
    try {
        const res = await fetch("/api/models");
        const data = await res.json();
        allModels = data.models || [];
        recommend();
    } catch (e) { document.getElementById("models").innerHTML = "<option>Ollama Offline</option>"; }
}

function recommend() {
    const task = document.getElementById("task").value;
    const sel = document.getElementById("models");
    sel.innerHTML = "";
    let filtered = allModels;

    if (task === "code") filtered = allModels.filter(m => m.name.toLowerCase().includes("coder") || m.name.toLowerCase().includes("code"));
    if (task === "fast") filtered = allModels.filter(m => m.details?.parameter_size?.includes("B") && parseFloat(m.details.parameter_size) <= 3.5);

    if (filtered.length === 0) filtered = allModels;
    filtered.forEach(m => {
        let opt = document.createElement("option");
        opt.value = m.name; opt.textContent = m.name;
        sel.appendChild(opt);
    });
}

async function generate() {
    const btn = document.getElementById("genBtn");
    const out = document.getElementById("output");
    btn.disabled = true; out.textContent = "Processing...";
    
    try {
        const res = await fetch("/api/generate", {
            method: "POST",
            body: JSON.stringify({ model: document.getElementById("models").value, prompt: document.getElementById("prompt").value, stream: false })
        });
        const data = await res.json();
        out.textContent = data.response;
    } catch (e) { out.textContent = "Error: " + e.message; }
    finally { btn.disabled = false; }
}

async function upload() {
    const data = new FormData();
    const input = document.getElementById("files");
    for (const f of input.files) data.append("files", f);
    const res = await fetch("/api/upload", {method:"POST", body:data});
    const json = await res.json();
    document.getElementById("upStatus").textContent = "Uploaded " + json.uploaded + " files.";
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
        with open(path, "w") as f: f.write(content)
    print("Project successfully created in 'webolla-v3/'")

if __name__ == "__main__": build()