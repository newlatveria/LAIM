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
	
	// 1. Check NVIDIA
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

	// 2. Check Intel (Fedora Robust Path)
	// Iterate through cards to find the Intel ARC
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("/sys/class/drm/card%d/device/", i)
		if _, err := os.Stat(path + "vendor"); err == nil {
			v, _ := os.ReadFile(path + "vendor")
			if strings.Contains(string(v), "0x8086") { // Intel Vendor ID
				data.GPUName = "Intel ARC A770"
				data.ActiveDev = "Intel GPU"
				
				// Try to get actual busy percentage from engine files
				if busy, err := os.ReadFile(path + "intel_gpu_busy"); err == nil {
					data.GPUUsage = strings.TrimSpace(string(busy)) + "%"
				} else {
					data.GPUUsage = "Monitoring..." 
				}
				
				// Get VRAM Usage via lmem (Local Memory)
				if lmem, err := os.ReadFile(path + "lmem_total_bytes"); err == nil {
					data.VRAMUsage = "Detected"
				}
				break
			}
		}
	}

send:
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "Invalid Request", 400); return
	}

	// Force disable streaming for browser compatibility
	reqBody["stream"] = false

	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		http.Error(w, "Ollama connection failed", 500); return
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(w, "Ollama result error", 500); return
	}

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
    # The Frontend: Restores Tasks and Uploads
    "webolla-v4/web/index.html": """<!doctype html>
<html>
<head>
    <title>WebOlla v4.5</title>
    <style>
        body { font-family: 'Segoe UI', sans-serif; max-width: 1000px; margin: 0 auto; background: #f4f4f9; padding: 20px; color: #333; }
        .card { background: white; padding: 20px; border-radius: 10px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); margin-bottom: 20px; }
        #output { background: #282c34; color: #abb2bf; padding: 20px; min-height: 250px; overflow-y: auto; border-radius: 5px; white-space: pre-wrap; font-family: 'Consolas', monospace; }
        .stat-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 15px; margin-bottom: 20px; }
        .stat-item { background: #fff; padding: 15px; border-radius: 8px; border-top: 4px solid #007bff; box-shadow: 0 2px 4px rgba(0,0,0,0.05); }
        .badge { background: #6f42c1; color: white; padding: 4px 12px; border-radius: 15px; font-size: 0.85em; }
        textarea { width: 100%; padding: 10px; border-radius: 5px; border: 1px solid #ccc; margin-top: 10px; }
        button { background: #007bff; color: white; border: none; padding: 10px 20px; border-radius: 5px; cursor: pointer; transition: 0.2s; }
        button:hover { background: #0056b3; }
        button:disabled { background: #ccc; }
    </style>
</head>
<body>
    <header style="display:flex; justify-content:space-between; align-items:center; margin-bottom:20px;">
        <h1>WebOlla <span class="badge">A770 Edition</span></h1>
        <div id="active-dev" style="font-weight:bold; color:#007bff;">Checking Hardware...</div>
    </header>

    <div class="stat-grid">
        <div class="stat-item"><strong>GPU:</strong> <span id="gpu-name">---</span></div>
        <div class="stat-item"><strong>Usage:</strong> <span id="gpu-usage">0%</span></div>
        <div class="stat-item"><strong>Speed:</strong> <span id="tps">0.00</span> t/s</div>
        <div class="stat-item"><strong>Latency:</strong> <span id="latency">0.0s</span></div>
    </div>

    <div class="card">
        <div style="display:flex; gap:10px; margin-bottom:15px;">
            <select id="task" onchange="recommend()" style="padding:8px; border-radius:4px;">
                <option value="advanced">Task: General</option>
                <option value="code">Task: Coding</option>
                <option value="fast">Task: Fast / 3B</option>
            </select>
            <select id="models" style="flex-grow:1; padding:8px; border-radius:4px;"></select>
        </div>
        <div id="output">System Ready. Select a model and ask a question.</div>
        <textarea id="prompt" rows="3" placeholder="What can I help with?"></textarea>
        <button onclick="generate()" id="genBtn" style="width:100%; margin-top:10px;">Run Inference</button>
    </div>

    <div class="card">
        <h3>File Management</h3>
        <input type="file" id="files" multiple>
        <button onclick="upload()">Upload Files</button>
        <span id="upStatus" style="margin-left:10px; font-weight:bold;"></span>
    </div>

<script>
let allModels = [];

async function init() {
    try {
        const res = await fetch("/api/models");
        const data = await res.json();
        allModels = data.models || [];
        recommend();
    } catch(e) { console.error("Ollama not responding"); }
    setInterval(tickTelemetry, 2000);
}

function recommend() {
    const task = document.getElementById("task").value;
    const sel = document.getElementById("models");
    sel.innerHTML = "";
    let filtered = allModels;
    if (task === "code") filtered = allModels.filter(m => m.name.toLowerCase().includes("code") || m.name.toLowerCase().includes("coder"));
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
        document.getElementById("active-dev").textContent = "Active: " + data.active_device;
    } catch(e) {}
}

async function generate() {
    const btn = document.getElementById("genBtn");
    const out = document.getElementById("output");
    const startTime = Date.now();
    btn.disabled = true;
    out.textContent = "Processing request...";

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
    const res = await fetch("/api/upload", { method: "POST", body: data });
    const json = await res.json();
    document.getElementById("upStatus").textContent = "Uploaded " + json.uploaded + " items.";
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
print("v4.5 Master Applied. Fixed JSON error, restored Task Recommend/Uploads, and improved A770 Probing.")