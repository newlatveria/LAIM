from zipfile import ZipFile
from pathlib import Path

base = Path("/mnt/data/webolla-fresh-v3")
(base / "cmd/webolla").mkdir(parents=True, exist_ok=True)
(base / "internal/handlers").mkdir(parents=True, exist_ok=True)
(base / "internal/config").mkdir(parents=True, exist_ok=True)
(base / "web").mkdir(parents=True, exist_ok=True)

(base / "go.mod").write_text("module webolla\n\ngo 1.22\n")

(base / "cmd/webolla/main.go").write_text("""package main

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

	log.Println("Listening on :" + cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
""")

(base / "internal/config/config.go").write_text("""package config

import "os"

type Config struct {
	Port          string
	OllamaBaseURL string
}

func Load() *Config {
	return &Config{
		Port:          env("PORT", "8080"),
		OllamaBaseURL: env("OLLAMA_BASE_URL", "http://127.0.0.1:11434"),
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
""")

(base / "internal/handlers/handlers.go").write_text("""package handlers

import (
	"bytes"
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

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	json.NewDecoder(r.Body).Decode(&req)

	buf := new(bytes.Buffer)
	json.NewEncoder(buf).Encode(req)

	resp, err := http.Post(
		h.cfg.OllamaBaseURL+"/api/generate",
		"application/json",
		buf,
	)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(64 << 20)
	files := r.MultipartForm.File["files"]
	os.MkdirAll("uploads", 0755)

	for _, f := range files {
		src, _ := f.Open()
		dst, _ := os.Create(filepath.Join("uploads", filepath.Base(f.Filename)))
		io.Copy(dst, src)
		src.Close()
		dst.Close()
	}

	json.NewEncoder(w).Encode(map[string]any{"uploaded": len(files)})
}
""")

(base / "web/index.html").write_text("""<!doctype html>
<html>
<head>
<meta charset="utf-8"/>
<title>WebOlla – Helpful Generate</title>
</head>
<body>

<h1>WebOlla</h1>

<h2>Generate</h2>

<label>Task</label>
<select id="task" onchange="recommend()">
  <option value="writing">Writing / Summarization</option>
  <option value="code">Code / Programming</option>
  <option value="reasoning">Reasoning / Q&A</option>
  <option value="fast">Fast / Lightweight</option>
  <option value="advanced">Advanced (show all)</option>
</select>

<h3>Models</h3>
<select id="models"></select>

<br><br>
<textarea id="prompt" rows="6" cols="60" placeholder="Enter prompt..."></textarea>
<br><br>
<button onclick="generate()">Generate</button>

<pre id="output"></pre>

<hr>

<h2>Upload Files / Folders</h2>
<input type="file" id="files" multiple>
<input type="file" id="folders" webkitdirectory multiple>
<button onclick="upload()">Upload</button>

<script>
let allModels = []

async function loadModels() {
  const res = await fetch("/api/models")
  const data = await res.json()
  allModels = data.models || []
  recommend()
}

function recommend() {
  const task = document.getElementById("task").value
  const sel = document.getElementById("models")
  sel.innerHTML = ""

  let filtered = allModels
  if (task === "code") filtered = allModels.filter(m => m.name.includes("coder"))
  if (task === "fast") filtered = allModels.filter(m => m.details?.parameter_size?.includes("B") && parseFloat(m.details.parameter_size) <= 3)
  if (task === "advanced") filtered = allModels

  if (filtered.length === 0) filtered = allModels

  for (const m of filtered) {
    const o = document.createElement("option")
    o.value = m.name
    o.textContent = m.name
    sel.appendChild(o)
  }
}

async function generate() {
  const model = document.getElementById("models").value
  const prompt = document.getElementById("prompt").value

  const res = await fetch("/api/generate", {
    method: "POST",
    headers: {"Content-Type":"application/json"},
    body: JSON.stringify({model, prompt})
  })

  document.getElementById("output").textContent = await res.text()
}

async function upload() {
  const data = new FormData()
  for (const f of files.files) data.append("files", f)
  for (const f of folders.files) data.append("files", f)
  await fetch("/api/upload", {method:"POST", body:data})
  alert("Uploaded")
}

loadModels()
</script>

</body>
</html>
""")

zip_path = "/mnt/data/webolla-fresh-v3.zip"
with ZipFile(zip_path, "w") as z:
	for p in base.rglob("*"):
		z.write(p, p.relative_to(base))

zip_path
