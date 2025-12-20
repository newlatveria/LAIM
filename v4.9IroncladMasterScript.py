import os

project_files = {
    "webolla-v4/go.mod": "module webolla\n\ngo 1.22",

    "webolla-v4/internal/handlers/handlers.go": """package handlers
import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
//	"os/exec"
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
	GPUName     string  `json:"gpu_name"`
	GPUUsage    int     `json:"gpu_usage_val"`
	VRAMUsage   float64 `json:"vram_gb"`
	ActiveDev   string  `json:"active_device"`
}

func New(cfg *config.Config) *Handlers {
	return &Handlers{cfg: cfg, client: &http.Client{Timeout: 120 * time.Second}}
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join("web", "index.html"))
}

func (h *Handlers) Telemetry(w http.ResponseWriter, r *http.Request) {
	data := TelemetryData{GPUName: "Detecting...", ActiveDev: "CPU"}
	
	// Intel ARC A770 Fedora Sysfs Logic
	for i := 0; i < 3; i++ {
		path := fmt.Sprintf("/sys/class/drm/card%d/device/", i)
		if vendor, err := os.ReadFile(path + "vendor"); err == nil && strings.Contains(string(vendor), "0x8086") {
			data.GPUName = "Intel ARC A770"
			data.ActiveDev = "Intel GPU"
			if busy, err := os.ReadFile(path + "intel_gpu_busy"); err == nil {
				val, _ := strconv.Atoi(strings.TrimSpace(string(busy)))
				data.GPUUsage = val
			}
			if vTotal, err := os.ReadFile(path + "lmem_total_bytes"); err == nil {
				val, _ := strconv.ParseUint(strings.TrimSpace(string(vTotal)), 10, 64)
				data.VRAMUsage = float64(val) / (1024 * 1024 * 1024)
			}
			break
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid request", 400); return
	}

	reqBody["stream"] = false // CRITICAL: Stop the NDJSON stream
	jsonData, _ := json.Marshal(reqBody)
	
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": "Ollama Unreachable"})
		return
	}
	defer resp.Body.Close()

	var res map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": "Ollama returned invalid data"})
		return
	}

	// TPS Calculation
	if count, ok1 := res["eval_count"].(float64); ok1 {
		if dur, ok2 := res["eval_duration"].(float64); ok2 && dur > 0 {
			res["tps"] = count / (dur / 1e9)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, err := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	if err != nil { http.Error(w, "offline", 502); return }
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
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

    "web/index.html": """<!doctype html>
<html>
<head>
    <title>WebOlla v4.9 Ironclad</title>
    <style>
        :root { --bg: #0b0e14; --panel: #161b22; --accent: #58a6ff; --green: #3fb950; --yellow: #d29922; --red: #f85149; }
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica; background: var(--bg); color: #c9d1d9; margin: 0; display: flex; height: 100vh; }
        
        .sidebar { width: 300px; background: var(--panel); border-right: 1px solid #30363d; padding: 20px; display: flex; flex-direction: column; gap: 20px; }
        .main { flex-grow: 1; display: flex; flex-direction: column; padding: 20px; gap: 20px; }
        
        .stat-box { background: #0d1117; border: 1px solid #30363d; padding: 12px; border-radius: 6px; }
        .stat-val { font-size: 1.2em; font-weight: bold; display: block; margin-top: 5px; }
        
        #output { flex-grow: 1; background: #0d1117; border: 1px solid #30363d; border-radius: 6px; padding: 20px; overflow-y: auto; white-space: pre-wrap; font-family: monospace; color: #e6edf3; }
        
        textarea { width: 100%; background: #0d1117; color: white; border: 1px solid #30363d; border-radius: 6px; padding: 12px; font-size: 14px; }
        button { background: #238636; color: white; border: none; padding: 10px; border-radius: 6px; cursor: pointer; font-weight: bold; }
        button:hover { background: #2ea043; }
        
        .low { color: var(--green); } .mid { color: var(--yellow); } .high { color: var(--red); }
    </style>
</head>
<body>
    <div class="sidebar">
        <h2>WebOlla <small style="font-size:0.5em">v4.9</small></h2>
        <div class="stat-box">GPU: <span id="gpu-name" class="stat-val">---</span></div>
        <div class="stat-box">Load: <span id="gpu-usage" class="stat-val">0%</span></div>
        <div class="stat-box">VRAM: <span id="vram" class="stat-val" style="color:var(--accent)">0.0 GB</span></div>
        <div class="stat-box">Speed: <span id="tps" class="stat-val" style="color:#bc8cff">0.00 t/s</span></div>
        <hr style="border:0; border-top:1px solid #30363d; width:100%">
        <select id="task" onchange="filterModels()" style="padding:8px; background:#0d1117; color:white; border:1px solid #30363d;">
            <option value="all">All Models</option>
            <option value="code">Coding</option>
        </select>
        <select id="models" style="padding:8px; background:#0d1117; color:white; border:1px solid #30363d;"></select>
    </div>

    <div class="main">
        <div id="output">System ready. Select a model and start chatting.</div>
        <div style="display:flex; flex-direction:column; gap:10px;">
            <textarea id="prompt" rows="3" placeholder="Send a message..."></textarea>
            <div style="display:flex; gap:10px;">
                <button id="genBtn" onclick="generate()" style="flex-grow:1;">Generate</button>
                <button onclick="document.getElementById('f').click()" style="background:#30363d;">Upload</button>
                <input type="file" id="f" hidden multiple onchange="upload()">
            </div>
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
        setInterval(telemetry, 2000);
    }

    function filterModels() {
        const t = document.getElementById("task").value;
        const s = document.getElementById("models");
        s.innerHTML = "";
        let list = (t === 'code') ? models.filter(m => m.name.includes("code")) : models;
        list.forEach(m => {
            let o = document.createElement("option"); o.value = m.name; o.textContent = m.name; s.appendChild(o);
        });
    }

    async function telemetry() {
        try {
            const res = await fetch("/api/telemetry");
            const d = await res.json();
            document.getElementById("gpu-name").textContent = d.gpu_name;
            const u = document.getElementById("gpu-usage");
            u.textContent = d.gpu_usage_val + "%";
            u.className = "stat-val " + (d.gpu_usage_val > 75 ? "high" : (d.gpu_usage_val > 30 ? "mid" : "low"));
            document.getElementById("vram").textContent = d.vram_gb.toFixed(1) + " GB";
        } catch(e) {}
    }

    async function generate() {
        const btn = document.getElementById("genBtn");
        const out = document.getElementById("output");
        btn.disabled = true; out.textContent = "Thinking...";
        try {
            const res = await fetch("/api/generate", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ model: document.getElementById("models").value, prompt: document.getElementById("prompt").value })
            });
            const text = await res.text();
            try {
                const d = JSON.parse(text);
                out.textContent = d.response || d.error || "No response";
                document.getElementById("tps").textContent = (d.tps || 0).toFixed(2) + " t/s";
            } catch(e) {
                out.textContent = "Raw response error: " + text;
            }
        } catch(e) { out.textContent = "Network Error: " + e.message; }
        finally { btn.disabled = false; }
    }

    async function upload() {
        const fd = new FormData();
        for (const f of document.getElementById("f").files) fd.append("files", f);
        await fetch("/api/upload", { method: "POST", body: fd });
        alert("Files uploaded");
    }
    init();
    </script>
</body>
</html>
"""
}

def build():
    # Ensure correct directory structure for webolla-v4 project
    for path, content in project_files.items():
        # Adjusted to ensure we are writing inside the current webolla-v4 folder
        actual_path = os.path.join(os.getcwd(), path) if "webolla-v4" in path else os.path.join(os.getcwd(), "webolla-v4", path)
        os.makedirs(os.path.dirname(actual_path), exist_ok=True)
        with open(actual_path, "w") as f: f.write(content)
    print("v4.9 Built. Run: go run cmd/webolla/main.go")

if __name__ == "__main__": build()