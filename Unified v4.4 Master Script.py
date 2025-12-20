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
	
	// 1. TRY NVIDIA (nvidia-smi)
	if out, err := exec.Command("nvidia-smi", "--query-gpu=name,utilization.gpu,memory.used", "--format=csv,noheader,nounits").Output(); err == nil {
		parts := strings.Split(string(out), ",")
		if len(parts) >= 3 {
			data.GPUName = strings.TrimSpace(parts[0])
			data.GPUUsage = strings.TrimSpace(parts[1]) + "%"
			data.VRAMUsage = strings.TrimSpace(parts[2]) + "MB"
			data.ActiveDev = "NVIDIA GPU"
		}
	} else {
		// 2. TRY INTEL ARC (Fedora/Linux Sysfs)
		// Try to find the Intel Card (usually card0 or card1)
		cardPath := "/sys/class/drm/card0/device/"
		if _, err := os.Stat(cardPath + "intel_gpu_busy"); err == nil {
			data.GPUName = "Intel ARC A770"
			
			// Get GPU Busy %
			busy, _ := os.ReadFile(cardPath + "intel_gpu_busy")
			data.GPUUsage = strings.TrimSpace(string(busy)) + "%"

			// Get VRAM Usage (Approximate for ARC via DRM)
			if vram, err := os.ReadFile("/sys/class/drm/card0/lmem_total_bytes"); err == nil {
				// We can calculate usage if we have the corresponding used_bytes file
                // For simplicity, noting it's detected
				data.VRAMUsage = "DirectX/Vulkan Active"
			}
			data.ActiveDev = "Intel GPU"
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	json.NewDecoder(r.Body).Decode(&reqBody)
	reqBody["stream"] = false

	jsonData, _ := json.Marshal(reqBody)
	resp, err := h.client.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", bytes.NewBuffer(jsonData))
	if err != nil { http.Error(w, "Ollama connection error", 500); return }
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
	resp, _ := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	defer resp.Body.Close()
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
    "webolla-v4/web/index.html": """"""
}

for path, content in project_files.items():
    if "