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
	data := TelemetryData{CPUUsage: "Active", GPUName: "None Detected", GPUUsage: "0%", VRAMUsage: "0MB", ActiveDev: "CPU"}
	
	// 1. Try NVIDIA
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,utilization.gpu,memory.used", "--format=csv,noheader,nounits").Output(); err == nil {
		parts := strings.Split(string(out), ",")
		if len(parts) >= 3 {
			data.GPUName = strings.TrimSpace(parts[0])
			data.GPUUsage = strings.TrimSpace(parts[1]) + "%"
			data.VRAMUsage = strings.TrimSpace(parts[2]) + "MB"
			data.ActiveDev = "NVIDIA GPU"
		}
	} else if _, err := exec.LookPath("intel_gpu_top"); err == nil {
        // 2. Try Intel (Intel ARC)
		data.GPUName = "Intel ARC"
		data.ActiveDev = "Intel GPU"
        // Parsing intel_gpu_top output is complex; for now, we confirm it's detected
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	json.NewDecoder(r.Body).Decode(&reqBody)
	reqBody["stream"] = false // Ensure we get one clean JSON object

	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil { http.Error(w, "Ollama connection error", 500); return }
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	// TPS Calculation
	count, ok1 := result["eval_count"].(float64)
	dur, ok2 := result["eval_duration"].(float64)
	if ok1 && ok2 && dur > 0 {
		result["tps"] = fmt.Sprintf("%.2f", count / (dur / 1e9))
	} else {
		result["tps"] = "0.00"
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, err := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	if err != nil { http.Error(w, "Ollama offline", 502); return }
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
    <title>WebOlla v4.2 Ultimate</title>
    <style>
        body { font-family: 'Segoe UI', sans-serif; max-width: 1100px; margin: 0 auto; background: #eceff1; color: #333; }
        header { background: #263238; color: white; padding: 15px 30px; display: flex; justify-content: space-between; border-radius: 0 0 8px 8px; }
        .dashboard { display: grid; grid-template-columns: 1fr 320px; gap: 20px; padding: 20px; }
        .card { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); margin-bottom: 20px; }
        #output { background: #fafafa; border: 1px solid #ddd; padding: 15px; height: 350px; overflow-y: auto; border-radius: 4px; white-space: pre-wrap; font-family: monospace; }
        .telemetry-item { display: flex; justify-content: space-between; margin: 10px 0; border-bottom: 1px solid #eee; padding-bottom: 5px; }
        .badge { background: #fb8c00; color: white; padding: 3px 10px; border-radius: 12px; font-size: 0.85em; }
        button { background: #1976d2; color: white; border: none; padding: 10px 20px; border-radius: 4px; cursor: pointer; }
        button:disabled { background: #ccc; }
    </style>
</head>
<body>

<header>
    <div style="font-size: 1.5em; font-weight: bold;">WebOlla <span style="font-size: 0.5em; vertical-align: middle;">v4.2</span></div>
    <div id="active-badge" class="badge">Initializing...</div>
</header>

<div class="dashboard">
    <div class="main-panel">
        <div class="card">
            <div style="display:flex; gap: 10px; margin-bottom: 15px;">
                <select id="task" onchange="recommend()">
                    <option value="advanced">Task: General</option>
                    <option value="code">Task: Coding</option>
                    <option value="fast">Task: Fast / 3B</option>
                </select>
                <select id="models" style="flex-grow:1;"></select>
            </div>
            <div id="output">Welcome. Type a prompt below.</div>
            <div style="margin-top:15px; display:flex; gap:10px;">
                <textarea id="prompt" style="flex-grow:1; padding:10px;" rows="3" placeholder="Enter prompt..."></textarea>
                <button onclick="generate()" id="genBtn">Send</button>
            </div>
        </div>

        <div class="card">
            <h3>Upload Knowledge Base</h3>
            <input type="file" id="files" multiple>
            <input type="file" id="folders" webkitdirectory multiple>
            <button onclick="upload()">Upload</button>
            <span id="upStatus" style="margin-left: 15px;"></span>
        </div>
    </div>

    <div class="sidebar">
        <div class="card">
            <h3>Hardware Stats</h3>
            <div class="telemetry-item"><span>GPU</span> <strong id="gpu-name">---</strong></div>
            <div class="telemetry-item"><span>Load</span> <strong id="gpu-usage">0%</strong></div>
            <div class="telemetry-item"><span>VRAM</span> <strong id="vram">0MB</strong></div>
        </div>

        <div class="card">
            <h3>Performance</h3>
            <div class="telemetry-item"><span>Speed</span> <strong><span id="tps">0.00</span> t/s</strong></div>
            <div class="telemetry-item"><span>Latency</span> <strong id="latency">0.0s</strong></div>
        </div>
    </div>
</div>

<script>
let allModels = [];

async function init() {
    try {
        const res = await fetch("/api/models");
        const data = await res.json();
        allModels = data.models || [];
        recommend();
    } catch(e) { console.error("Could not load models"); }
    setInterval(tickTelemetry, 2000);
}

function recommend() {
    const task = document.getElementById("task").value;
    const sel = document.getElementById("models");
    sel.innerHTML = "";
    let filtered = allModels;
    if (task === "code") filtered = allModels.filter(m => m.name.includes("coder") || m.name.includes("code"));
    if (task === "fast") filtered = allModels.filter(m => m.details?.parameter_size?.includes("B") && parseFloat(m.details.parameter_size) <= 3.5);
    
    if (filtered.length === 0) filtered = allModels;
    filtered.forEach(m => {
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
        document.getElementById("active-badge").textContent = "PROCESSED BY: " + data.active_device;
    } catch(e) {}
}

async function generate() {
    const btn = document.getElementById("genBtn");
    const out = document.getElementById("output");
    const p = document.getElementById("prompt").value;
    if(!p) return;

    btn.disabled = true;
    out.textContent = "Thinking...";
    const start = Date.now();

    try {
        const res = await fetch("/api/generate", {
            method: "POST",
            body: JSON.stringify({ model: document.getElementById("models").value, prompt: p })
        });
        const data = await res.json();
        out.textContent = data.response;
        document.getElementById("tps").textContent = data.tps;
        document.getElementById("latency").textContent = ((Date.now() - start)/1000).toFixed(1) + "s";
    } catch(e) { out.textContent = "Error: " + e.message; }
    finally { btn.disabled = false; }
}

async function upload() {
    const data = new FormData();
    const f1 = document.getElementById("files").files;
    const f2 = document.getElementById("folders").files;
    for (const f of f1) data.append("files", f);
    for (const f of f2) data.append("files", f);
    const res = await fetch("/api/upload", { method: "POST", body: data });
    const json = await res.json();
    document.getElementById("upStatus").textContent = "Success: " + json.uploaded + " files.";
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
print("Project v4.2 built successfully. Task recommendations, Uploads, and Telemetry fixed.")