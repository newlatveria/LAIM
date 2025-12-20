import os

project_files = {
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
	
	// Probe NVIDIA
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,utilization.gpu,memory.used", "--format=csv,noheader,nounits").Output(); err == nil {
		parts := strings.Split(string(out), ",")
		if len(parts) >= 3 {
			data.GPUName = strings.TrimSpace(parts[0])
			data.GPUUsage = strings.TrimSpace(parts[1]) + "%"
			data.VRAMUsage = strings.TrimSpace(parts[2]) + "MB"
			data.ActiveDev = "NVIDIA GPU"
		}
	} else if _, err := exec.LookPath("intel_gpu_top"); err == nil {
		data.GPUName = "Intel ARC"
		data.ActiveDev = "Intel GPU"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "JSON Decode Error", 400); return
	}

	// MANDATORY: Disable streaming to fix the JSON.parse error
	reqBody["stream"] = false

	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		http.Error(w, "Ollama Unreachable", 500); return
	}
	defer resp.Body.Close()

	// Capture response as a single map
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(w, "Ollama sent invalid JSON", 500); return
	}

	// Add Performance Metrics
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
    # The Frontend remains largely the same but ensures it captures 'tps' correctly
    "webolla-v4/web/index.html": """<!doctype html>
<html>
<head>
    <title>WebOlla v4.3</title>
    <style>
        body { font-family: sans-serif; max-width: 1000px; margin: 0 auto; background: #f0f2f5; padding: 20px; }
        .card { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); margin-bottom: 20px; }
        #output { background: #1e1e1e; color: #d4d4d4; padding: 15px; height: 300px; overflow-y: auto; border-radius: 4px; white-space: pre-wrap; font-family: monospace; }
        .stats { display: flex; gap: 20px; }
        .stat-box { flex: 1; background: #fff; padding: 15px; border-radius: 8px; border-left: 5px solid #007bff; }
        .badge { background: #28a745; color: white; padding: 5px 10px; border-radius: 20px; font-size: 0.8em; }
    </style>
</head>
<body>
    <h1>WebOlla <span id="active-dev" class="badge">Detecting Hardware...</span></h1>

    <div class="stats">
        <div class="stat-box">
            <strong>GPU:</strong> <span id="gpu-name">---</span><br>
            <strong>Usage:</strong> <span id="gpu-usage">0%</span> | <span id="vram">0MB</span>
        </div>
        <div class="stat-box">
            <strong>Speed:</strong> <span id="tps">0.00</span> t/s<br>
            <strong>Latency:</strong> <span id="latency">0.0s</span>
        </div>
    </div>

    <div class="card" style="margin-top:20px;">
        <div style="display:flex; gap:10px; margin-bottom:10px;">
            <select id="task" onchange="recommend()"><option value="advanced">General</option><option value="code">Coding</option><option value="fast">Fast</option></select>
            <select id="models" style="flex-grow:1;"></select>
        </div>
        <div id="output">System Ready.</div>
        <textarea id="prompt" style="width:100%; margin-top:10px;" rows="3" placeholder="Enter message..."></textarea>
        <button onclick="generate()" id="genBtn" style="width:100%; padding:10px; margin-top:10px; background:#007bff; color:white; border:none; border-radius:4px;">Generate</button>
    </div>

    <div class="card">
        <h3>Knowledge Upload</h3>
        <input type="file" id="files" multiple>
        <button onclick="upload()">Upload</button>
    </div>

<script>
let allModels = [];
async function init() {
    try {
        const res = await fetch("/api/models");
        const data = await res.json();
        allModels = data.models || [];
        recommend();
    } catch(e) {}
    setInterval(tickTelemetry, 2000);
}

function recommend() {
    const task = document.getElementById("task").value;
    const sel = document.getElementById("models");
    sel.innerHTML = "";
    let filtered = allModels;
    if (task === "code") filtered = allModels.filter(m => m.name.includes("code"));
    if (task === "fast") filtered = allModels.filter(m => m.details?.parameter_size?.includes("B") && parseFloat(m.details.parameter_size) <= 4);
    
    (filtered.length ? filtered : allModels).forEach(m => {
        let o = document.createElement("option"); o.value = m.name; o.textContent = m.name; sel.appendChild(o);
    });
}

async function tickTelemetry() {
    try {
        const res = await fetch("/api/telemetry");
        const data = await res.json();
        document.getElementById("gpu-name").textContent = data.gpu_name;
        document.getElementById("gpu-usage").textContent = data.gpu_usage;
        document.getElementById("vram").textContent = data.vram;
        document.getElementById("active-dev").textContent = "MODE: " + data.active_device;
    } catch(e) {}
}

async function generate() {
    const btn = document.getElementById("genBtn");
    const out = document.getElementById("output");
    const startTime = Date.now();
    btn.disabled = true;
    out.textContent = "Processing...";

    try {
        const res = await fetch("/api/generate", {
            method: "POST",
            body: JSON.stringify({ model: document.getElementById("models").value, prompt: document.getElementById("prompt").value })
        });
        const data = await res.json();
        out.textContent = data.response;
        document.getElementById("tps").textContent = data.tps || "0.00";
        document.getElementById("latency").textContent = ((Date.now() - startTime)/1000).toFixed(1) + "s";
    } catch(e) { out.textContent = "Error: " + e.message; }
    finally { btn.disabled = false; }
}

async function upload() {
    const data = new FormData();
    for (const f of document.getElementById("files").files) data.append("files", f);
    await fetch("/api/upload", { method: "POST", body: data });
    alert("Uploaded successfully");
}

init();
</script>
</body>
</html>
"""
}

for path, content in project_files.items():
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f: f.write(content)
print("v4.3 Master Script Applied. Fixed JSON stream error and restored all features.")