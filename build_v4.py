import os

project_files = {
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
	log.Printf("V4 Dashboard: http://localhost:%s\\n", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, mux))
}
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
	"runtime"
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
	return &Handlers{cfg: cfg, client: &http.Client{Timeout: 60 * time.Second}}
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join("web", "index.html"))
}

// Telemetry Logic: Detects Hardware
func (h *Handlers) Telemetry(w http.ResponseWriter, r *http.Request) {
	data := TelemetryData{CPUUsage: "0%", GPUName: "None Detected", GPUUsage: "0%", VRAMUsage: "0MB", ActiveDev: "CPU"}
	
	// 1. Check NVIDIA
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,utilization.gpu,memory.used", "--format=csv,noheader,nounits").Output(); err == None {
		parts := strings.Split(string(out), ",")
		if len(parts) >= 3 {
			data.GPUName, data.GPUUsage, data.VRAMUsage = parts[0], parts[1]+"%", parts[2]+"MB"
			if strings.TrimSpace(parts[1]) != "0" { data.ActiveDev = "NVIDIA GPU" }
		}
	} else if _, err := exec.LookPath("intel_gpu_top"); err == nil {
        data.GPUName = "Intel ARC / Iris"
        data.ActiveDev = "Intel GPU"
    }

	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	var reqBody map[string]any
	json.NewDecoder(r.Body).Decode(&reqBody)
	
	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil { http.Error(w, "Ollama Down", 500); return }
	defer resp.Body.Close()

	// Capture the raw response to calculate TPS
	var result map[string]any
	bodyBytes, _ := io.ReadAll(resp.Body)
	json.Unmarshal(bodyBytes, &result)

	duration := time.Since(start).Seconds()
	evalCount, _ := result["eval_count"].(float64)
	tps := 0.0
	if duration > 0 { tps = evalCount / duration }

	result["tps"] = fmt.Sprintf("%.2f", tps)
	result["latency"] = fmt.Sprintf("%.2fs", duration)
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, _ := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	defer resp.Body.Close()
	io.Copy(w, resp.Body)
}

func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(64 << 20)
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

    "webolla-v4/web/index.html": """<!doctype html>
<html>
<head>
<title>WebOlla v4 Pro</title>
<style>
    body { font-family: 'Segoe UI', sans-serif; max-width: 1000px; margin: 0 auto; background: #f4f7f6; display: flex; flex-direction: column; height: 100vh; }
    header { background: #2c3e50; color: white; padding: 1rem; display: flex; justify-content: space-between; align-items: center; }
    .main-container { display: grid; grid-template-columns: 1fr 300px; gap: 20px; padding: 20px; flex-grow: 1; }
    .chat-box { background: white; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1); padding: 20px; display: flex; flex-direction: column; }
    .sidebar { display: flex; flex-direction: column; gap: 20px; }
    .stat-card { background: white; padding: 15px; border-radius: 8px; box-shadow: 0 2px 5px rgba(0,0,0,0.05); }
    .badge { padding: 4px 8px; border-radius: 4px; font-size: 0.8rem; font-weight: bold; background: #e67e22; color: white; }
    textarea { width: 100%; border: 1px solid #ddd; border-radius: 4px; padding: 10px; resize: none; }
    #output { background: #f9f9f9; padding: 15px; border-radius: 4px; white-space: pre-wrap; flex-grow: 1; overflow-y: auto; margin-bottom: 10px; border: 1px solid #eee; }
    .loader { color: #3498db; font-weight: bold; }
</style>
</head>
<body>

<header>
    <div><strong>WebOlla v4</strong> <span id="active-dev" class="badge">Detecting...</span></div>
    <div id="hw-strip">CPU: -- | GPU: --</div>
</header>

<div class="main-container">
    <div class="chat-box">
        <div id="output">Waiting for prompt...</div>
        <div style="margin-top: 10px;">
            <textarea id="prompt" rows="3" placeholder="Ask anything..."></textarea>
            <div style="display:flex; justify-content: space-between; align-items: center; margin-top: 10px;">
                <div>
                    <select id="models" style="padding: 8px;"></select>
                </div>
                <button onclick="generate()" id="btn" style="padding: 10px 30px; background: #27ae60; color: white; border: none; border-radius: 4px; cursor: pointer;">Generate</button>
            </div>
        </div>
    </div>

    <div class="sidebar">
        <div class="stat-card">
            <h4>Live Telemetry</h4>
            <p><strong>GPU:</strong> <span id="gpu-name">Detecting...</span></p>
            <p><strong>Load:</strong> <span id="gpu-load">0%</span></p>
            <p><strong>VRAM:</strong> <span id="vram">0MB</span></p>
        </div>
        <div class="stat-card">
            <h4>Inference Stats</h4>
            <p><strong>Speed:</strong> <span id="tps">0.00</span> t/s</p>
            <p><strong>Latency:</strong> <span id="latency">0s</span></p>
        </div>
    </div>
</div>

<script>
async function updateTelemetry() {
    try {
        const res = await fetch("/api/telemetry");
        const data = await res.json();
        document.getElementById("gpu-name").textContent = data.gpu_name;
        document.getElementById("gpu-load").textContent = data.gpu_usage;
        document.getElementById("vram").textContent = data.vram;
        document.getElementById("active-dev").textContent = "RUNNING ON: " + data.active_device;
        document.getElementById("hw-strip").textContent = `CPU: ${data.cpu} | GPU: ${data.gpu_usage}`;
    } catch(e) {}
}

async function generate() {
    const out = document.getElementById("output");
    const btn = document.getElementById("btn");
    out.innerHTML = '<span class="loader">Generating response...</span>';
    btn.disabled = true;

    try {
        const res = await fetch("/api/generate", {
            method: "POST",
            body: JSON.stringify({ model: document.getElementById("models").value, prompt: document.getElementById("prompt").value, stream: false })
        });
        const data = await res.json();
        out.textContent = data.response;
        document.getElementById("tps").textContent = data.tps;
        document.getElementById("latency").textContent = data.latency;
    } catch(e) { out.textContent = "Error: " + e.message; }
    finally { btn.disabled = false; }
}

async function init() {
    const res = await fetch("/api/models");
    const data = await res.json();
    const sel = document.getElementById("models");
    data.models?.forEach(m => {
        let o = document.createElement("option"); o.value = m.name; o.textContent = m.name; sel.appendChild(o);
    });
    setInterval(updateTelemetry, 2000);
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
    print("Project 'webolla-v4' with GPU Telemetry is ready!")

if __name__ == "__main__": build()