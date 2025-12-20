import os

# Updated handlers file fixing the 'None' typo and 'runtime' import
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
	return &Handlers{cfg: cfg, client: &http.Client{Timeout: 60 * time.Second}}
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join("web", "index.html"))
}

func (h *Handlers) Telemetry(w http.ResponseWriter, r *http.Request) {
	data := TelemetryData{CPUUsage: "N/A", GPUName: "None Detected", GPUUsage: "0%", VRAMUsage: "0MB", ActiveDev: "CPU"}
	
	// Check NVIDIA
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,utilization.gpu,memory.used", "--format=csv,noheader,nounits").Output(); err == nil {
		parts := strings.Split(string(out), ",")
		if len(parts) >= 3 {
			data.GPUName, data.GPUUsage, data.VRAMUsage = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])+"%", strings.TrimSpace(parts[2])+"MB"
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
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
        http.Error(w, "Invalid JSON", 400); return
    }
	
	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil { http.Error(w, "Ollama Down", 500); return }
	defer resp.Body.Close()

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
	resp, err := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
    if err != nil { http.Error(w, "Ollama Offline", 502); return }
	defer resp.Body.Close()
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
			src, err := f.Open(); if err != nil { return }
            defer src.Close()
			dst, err := os.Create(filepath.Join(h.cfg.UploadDir, filepath.Base(f.Filename)))
			if err != nil { return }
            defer dst.Close()
			io.Copy(dst, src)
		}()
	}
	json.NewEncoder(w).Encode(map[string]any{"uploaded": len(files)})
}
"""
}

# Apply the fix
for path, content in project_files.items():
    with open(path, "w") as f: f.write(content)
print("Fixed typo and unused import in handlers.go")