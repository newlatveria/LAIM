import os

# Project configuration and source code
project_files = {
    # CORRECTED go.mod: Single newline and proper module path
    "webolla-v4/go.mod": "module webolla\n\ngo 1.22",

    "webolla-v4/cmd/webolla/main.go": """package main
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
	mux.HandleFunc("/api/telemetry", h.Telemetry)
	log.Printf("Server starting on http://localhost:%s\\n", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
""",

    "webolla-v4/internal/config/config.go": """package config
import "os"
type Config struct { Port, OllamaBaseURL, UploadDir string }
func Load() *Config {
	return &Config{
		Port: env("PORT", "8080"),
		OllamaBaseURL: env("OLLAMA_BASE_URL", "http://127.0.0.1:11434"),
		UploadDir: env("UPLOAD_DIR", "uploads"),
	}
}
func env(k, d string) string { if v := os.Getenv(k); v != "" { return v }; return d }
""",

    "webolla-v4/internal/handlers/handlers.go": """package handlers
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"webolla/internal/config"
)

type Handlers struct {
	cfg    *config.Config
	client *http.Client
}

type TelemetryData struct {
	CPUUsage    string `json:"cpu"`
	GPUName     string `json:"gpu_name"`
	GPUUsage    string `json:"gpu_usage"`
	VRAMUsage   string `json:"vram"`
	ActiveDev   string `json:"active_device"`
}

func New(cfg *config.Config) *Handlers {
	return &Handlers{cfg: cfg, client: &http.Client{Timeout: 90 * time.Second}}
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join("web", "index.html"))
}

func (h *Handlers) Telemetry(w http.ResponseWriter, r *http.Request) {
	data := TelemetryData{CPUUsage: "Active", GPUName: "None", GPUUsage: "0%", VRAMUsage: "0MB", ActiveDev: "CPU"}
	
	// 1. Try NVIDIA
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,utilization.gpu,memory.used", "--format=csv,noheader,nounits").Output(); err == nil {
		parts := strings.Split(string(out), ",")
		if len(parts) >= 3 {
			data.GPUName = strings.TrimSpace(parts[0])
			data.GPUUsage = strings.TrimSpace(parts[1]) + "%"
			data.VRAMUsage = strings.TrimSpace(parts[2]) + "MB"
			data.ActiveDev = "NVIDIA GPU"
			goto send
		}
	}

	// 2. Try Intel ARC (Fedora Path)
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("/sys/class/drm/card%d/device/", i)
		if vendor, err := os.ReadFile(path + "vendor"); err == nil && strings.Contains(string(vendor), "0x8086") {
			data.GPUName = "Intel ARC A770"
			data.ActiveDev = "Intel GPU"
			
			if busy, err := os.ReadFile(path + "intel_gpu_busy"); err == nil {
				data.GPUUsage = strings.TrimSpace(string(busy)) + "%"
			}
			
			if vBytes, err := os.ReadFile(path + "lmem_total_bytes"); err == nil {
				val, _ := strconv.ParseUint(strings.TrimSpace(string(vBytes)), 10, 64)
				data.VRAMUsage = fmt.Sprintf("%.1f GB Total", float64(val)/(1024*1024*1024))
			}
			break
		}
	}

send:
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Bad Request", 400); return
	}
	reqBody["stream"] = false
	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil { http.Error(w, "Ollama Offline", 500); return }
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	count, ok1 := result["eval_count"].(float64)
	dur, ok2 := result["eval_duration"].(float64)
	if ok1 && ok2 && dur > 0 {
		result["tps"] = fmt.Sprintf("%.2f", count / (dur / 1e9))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, err := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	if err != nil { http.Error(w, "Offline", 502); return }
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(500 << 20)
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
	json.NewEncoder(w).Encode(map[string]interface{}{"uploaded": len(files)})
}
""",

    "webolla-v4/web/index.html": """<!doctype html>
<html>
<head>
    <title>WebOlla v4.7</title>
    <style>
        body { font-family: sans-serif; max-width: 900px; margin: 0 auto; background: #f4f4f9; padding: 20px; }
        .card { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); margin-bottom: 20px; }
        #output { background: #222; color: #eee; padding: 15px; min-height: 200px; border-radius: 5px; white-space: pre-wrap; margin-top: 10px; }
        .stats { display: flex; gap: 10px; margin-bottom: 20px; }
        .stat { flex: 1; background: white; padding: 10px; border-radius: 5px; text-align: center; border-top: 3px solid #007bff; }
        textarea { width: 100%; padding: 10px; margin-top: 10px; border-radius: 5px; border: 1px solid #ccc; }
        button { background: #007bff; color: white; border: none; padding: 10px 20px; border-radius: 5px; cursor: pointer; font-weight: bold; }
        button:disabled { background: #999; }
    </style>
</head>
<body>
    <h1>WebOlla <small id="active-dev" style="font-size: 0.5em; background: #666; color: white; padding: 2px 8px; border-radius: 10px;">Checking...</small></h1>
    
    <div class="stats">
        <div class="stat"><strong>GPU</strong><br><span id="gpu-name">None</span></div>
        <div class="stat"><strong>Load</strong><br><span id="gpu-usage">0%</span></div>
        <div class="stat"><strong>VRAM</strong><br><span id="vram">0MB</span></div>
        <div class="stat"><strong>TPS</strong><br><span id="tps">0.00</span></div>
    </div>

    <div class="card">
        <select id="models" style="width: 100%; padding: 10px; margin-bottom: 10px;"></select>
        <div id="output">Output will appear here...</div>
        <textarea id="prompt" rows="3" placeholder="Enter prompt..."></textarea>
        <button onclick="generate()" id="genBtn">Generate</button>
    </div>

    <script>
    async function init() {
        const res = await fetch("/api/models");
        const data = await res.json();
        const sel = document.getElementById("models");
        (data.models || []).forEach(m => {
            let o = document.createElement("option"); o.value = m.name; o.textContent = m.name; sel.appendChild(o);
        });
        setInterval(telemetry, 2000);
    }

    async function telemetry() {
        const res = await fetch("/api/telemetry");
        const data = await res.json();
        document.getElementById("gpu-name").textContent = data.gpu_name;
        document.getElementById("gpu-usage").textContent = data.gpu_usage;
        document.getElementById("vram").textContent = data.vram;
        document.getElementById("active-dev").textContent = data.active_device;
    }

    async function generate() {
        const btn = document.getElementById("genBtn");
        btn.disabled = true;
        document.getElementById("output").textContent = "Thinking...";
        const res = await fetch("/api/generate", {
            method: "POST",
            body: JSON.stringify({ model: document.getElementById("models").value, prompt: document.getElementById("prompt").value })
        });
        const data = await res.json();
        document.getElementById("output").textContent = data.response;
        document.getElementById("tps").textContent = data.tps || "0.00";
        btn.disabled = false;
    }
    init();
    </script>
</body>
</html>
"""
}

def build():
    for path, content in project_files.items():
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w") as f: f.write(content)
    print("Project v4.7 Fixed Build Complete.")
    print("Run: cd webolla-v4 && go run cmd/webolla/main.go")

if __name__ == "__main__": build()