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
	
	// NVIDIA Detection
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
		http.Error(w, "Bad Request", 400); return
	}

	// Force stream to false so we get one clean JSON object back from Ollama
	reqBody["stream"] = false

	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		http.Error(w, "Ollama connection failed", 500); return
	}
	defer resp.Body.Close()

	// Use a decoder to handle the response body properly
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		http.Error(w, "Failed to parse Ollama response", 500); return
	}

	// Calculate Tokens Per Second (TPS)
	// Ollama returns eval_duration in nanoseconds
	evalCount, ok1 := result["eval_count"].(float64)
	evalDuration, ok2 := result["eval_duration"].(float64)
	
	tps := 0.0
	if ok1 && ok2 && evalDuration > 0 {
		tps = evalCount / (evalDuration / 1e9)
	}

	result["tps"] = fmt.Sprintf("%.2f", tps)
	
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
	r.ParseMultipartForm(100 << 20)
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
"""
}

for path, content in project_files.items():
    with open(path, "w") as f: f.write(content)
print("v4.1 Fix Applied: Fixed JSON parsing and removed unused runtime import.")