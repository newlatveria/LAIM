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
	log.Printf("WebOlla running at http://localhost:%s\\n", cfg.Port)
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
	GPUUsage    int    `json:"gpu_usage_val"`
	VRAMUsage   float64 `json:"vram_gb"`
	ActiveDev   string `json:"active_device"`
}

func New(cfg *config.Config) *Handlers {
	return &Handlers{cfg: cfg, client: &http.Client{Timeout: 90 * time.Second}}
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join("web", "index.html"))
}

func (h *Handlers) Telemetry(w http.ResponseWriter, r *http.Request) {
	data := TelemetryData{CPUUsage: "Active", GPUName: "None", ActiveDev: "CPU"}
	
	// NVIDIA Check
	if out, err := exec.Command("nvidia-smi", "--query-gpu=utilization.gpu,memory.total", "--format=csv,noheader,nounits").Output(); err == nil {
		parts := strings.Split(string(out), ",")
		if len(parts) >= 2 {
			u, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
			v, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			data.GPUName = "NVIDIA GPU"
			data.GPUUsage = u
			data.VRAMUsage = v / 1024
			data.ActiveDev = "NVIDIA"
			goto send
		}
	}

	// Intel ARC A770 Probing
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("/sys/class/drm/card%d/device/", i)
		if vendor, err := os.ReadFile(path + "vendor"); err == nil && strings.Contains(string(vendor), "0x8086") {
			data.GPUName = "Intel ARC A770"
			data.ActiveDev = "Intel GPU"
			if busy, err := os.ReadFile(path + "intel_gpu_busy"); err == nil {
				val, _ := strconv.Atoi(strings.TrimSpace(string(busy)))
				data.GPUUsage = val
			}
			if vBytes, err := os.ReadFile(path + "lmem_total_bytes"); err == nil {
				val, _ := strconv.ParseUint(strings.TrimSpace(string(vBytes)), 10, 64)
				data.VRAMUsage = float64(val) / (1024 * 1024 * 1024)
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
	json.NewDecoder(r.Body).Decode(&reqBody)
	reqBody["stream"] = false
	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil { http.Error(w, "Ollama Error", 500); return }
	defer resp.Body.Close()
	var res map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&res)
	
	count, ok1 := res["eval_count"].(float64)
	dur, ok2 := res["eval_duration"].(float64)
	if ok1 && ok2 && dur > 0 {
		res["tps"] = count / (dur / 1e9)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, _ := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	defer resp.Body.Close()
	io.Copy(w, resp.Body)
}

func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(500 << 20)
	files := r.MultipartForm.File["files"]
	os.MkdirAll(h.cfg.UploadDir, 0755)
	for _, f := range files {
		src, _ := f.Open()
		dst, _ := os.Create(filepath.Join(h.cfg.UploadDir, f.Filename))
		io.Copy(dst, src)
		src.Close(); dst.Close()
	}
	json.NewEncoder(w).Encode(map[string]int{"uploaded": len(files)})
}
""",

    "webolla-v4/web/index.html": """<!doctype html>
<html>
<head>
    <title>WebOlla v4.8 Complete</title>
    <style>
        :root { --bg: #0f172a; --card: #1e293b; --text: #f1f5f9; --primary: #3b82f6; }
        body { font-family: 'Inter', sans-serif; background: var(--bg); color: var(--text); max-width: 1100px; margin: 0 auto; padding: 20px; }
        .grid { display: grid; grid-template-columns: repeat(4, 1fr); gap: 15px; margin-bottom: 20px; }
        .stat-card { background: var(--card); padding: 15px; border-radius: 12px; text-align: center; border-bottom: 4px solid #444; transition: 0.3s; }
        #output { background: #000; color: #10b981; padding: 20px; border-radius: 10px; min-height: 350px; white-space: pre-wrap; font-family: 'Fira Code', monospace; font-size: 0.9em; overflow-y: auto; border: 1px solid #334155; }
        .controls { background: var(--card); padding: 20px; border-radius: 12px; display: flex; flex-direction: column; gap: 15px; }
        textarea { background: #0f172a; color: white; border: 1px solid #334155; border-radius: 8px; padding: 12px; resize: none; width: 100%; box-sizing: border-box; }
        button { background: var(--primary); color: white; border: none; padding: 12px; border-radius: 8px; font-weight: bold; cursor: pointer; }
        button:disabled { opacity: 0.5; }
        .low { color: #22c55e; } .mid { color: #eab308; } .high { color: #ef4444; }
        .vram-active { color: #06b6d4; } .tps-active { color: #a855f7; }
    </style>
</head>
<body>
    <header style="display:flex; justify-content:space-between; align-items:center; margin-bottom:20px;">
        <h1>WebOlla <span id="active-dev" style="font-size:0.4em; padding:4px 10px; border-radius:20px; background:var(--primary);">Detecting...</span></h1>
        <div id="upStatus" style="font-size:0.8em; color:#94a3b8;"></div>
    </header>

    <div class="grid">
        <div class="stat-card"><strong>Device</strong><br><span id="gpu-name">---</span></div>
        <div class="stat-card"><strong>GPU Load</strong><br><span id="gpu-usage" class="low">0%</span></div>
        <div class="stat-card"><strong>VRAM</strong><br><span id="vram" class="vram-active">0.0 GB</span></div>
        <div class="stat-card"><strong>Tokens/s</strong><br><span id="tps" class="tps-active">0.00</span></div>
    </div>

    <div class="controls">
        <div style="display:flex; gap:10px;">
            <select id="task" onchange="filterModels()" style="padding:8px; border-radius:5px; background:#0f172a; color:white;">
                <option value="all">All Models</option>
                <option value="code">Coding</option>
                <option value="chat">Chat/General</option>
            </select>
            <select id="models" style="flex-grow:1; padding:8px; border-radius:5px; background:#0f172a; color:white;"></select>
        </div>
        <div id="output">Ready. Select a model and send a message.</div>
        <textarea id="prompt" rows="3" placeholder="Ask Ollama something..."></textarea>
        <div style="display:flex; gap:10px;">
            <button id="genBtn" onclick="generate()" style="flex-grow:4;">Generate Response</button>
            <input type="file" id="fileInput" hidden multiple onchange="upload()">
            <button onclick="document.getElementById('fileInput').click()" style="background:#475569; flex-grow:1;">Upload Docs</button>
        </div>
    </div>

    <script>
    let models = [];
    async function init() {
        try {
            const res = await fetch("/api/models");
            const data = await res.json();
            models = data.models || [];
            filterModels();
        } catch(e) {}
        setInterval(updateTelemetry, 1500);
    }

    function filterModels() {
        const task = document.getElementById("task").value;
        const sel = document.getElementById("models");
        sel.innerHTML = "";
        let list = models;
        if(task === 'code') list = models.filter(m => m.name.includes("code") || m.name.includes("coder"));
        list.forEach(m => {
            let o = document.createElement("option"); o.value = m.name; o.textContent = m.name; sel.appendChild(o);
        });
    }

    async function updateTelemetry() {
        const res = await fetch("/api/telemetry");
        const d = await res.json();
        document.getElementById("gpu-name").textContent = d.gpu_name;
        document.getElementById("active-dev").textContent = d.active_device;
        
        const usageEl = document.getElementById("gpu-usage");
        usageEl.textContent = d.gpu_usage_val + "%";
        usageEl.className = d.gpu_usage_val > 70 ? "high" : (d.gpu_usage_val > 30 ? "mid" : "low");
        
        document.getElementById("vram").textContent = d.vram_gb.toFixed(1) + " GB";
    }

    async function generate() {
        const btn = document.getElementById("genBtn");
        const out = document.getElementById("output");
        btn.disabled = true; out.textContent = "Processing request...";
        try {
            const res = await fetch("/api/generate", {
                method: "POST",
                body: JSON.stringify({ model: document.getElementById("models").value, prompt: document.getElementById("prompt").value })
            });
            const d = await res.json();
            out.textContent = d.response;
            document.getElementById("tps").textContent = (d.tps || 0).toFixed(2);
        } catch(e) { out.textContent = "Error: " + e.message; }
        finally { btn.disabled = false; }
    }

    async function upload() {
        const formData = new FormData();
        for (const f of document.getElementById("fileInput").files) formData.append("files", f);
        const res = await fetch("/api/upload", { method: "POST", body: formData });
        const d = await res.json();
        document.getElementById("upStatus").textContent = "Uploaded " + d.uploaded + " files.";
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
    print("Project v4.8 BUILD COMPLETE. restored features & colorized stats.")
    print("Run: cd webolla-v4 && go run cmd/webolla/main.go")

if __name__ == "__main__": build()