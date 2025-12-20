package handlers
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
