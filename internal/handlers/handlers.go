package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

	CPUUsage  float64 `json:"cpu_usage"`
	RAMUsedGB float64 `json:"ram_used_gb"`
}

func New(cfg *config.Config) *Handlers {
	// Ensure data directories exist on startup
	os.MkdirAll(filepath.Join(cfg.UploadDir), 0755)
	os.MkdirAll("data/rag", 0755)
	
	return &Handlers{cfg: cfg, client: &http.Client{Timeout: 120 * time.Second}}
}

func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join("web", "index.html"))
}

/* ==================== TELEMETRY ==================== */

/* ---- CPU state ---- */
var (
	lastCPUIdle  uint64
	lastCPUTotal uint64
	cpuInit      bool
)

/* ---- Intel GPU engine state ---- */
var (
	lastGPUBusy uint64
	lastGPUTime time.Time
	gpuInit     bool
)

func (h *Handlers) Telemetry(w http.ResponseWriter, r *http.Request) {
	data := TelemetryData{
		GPUName:   "None",
		ActiveDev: "CPU",
		GPUUsage:  0,
	}

	detectGPU(&data)
	data.CPUUsage = readCPUUsage()
	data.RAMUsedGB = readRAMUsedGB()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

/* ---------------- GPU ---------------- */

func detectGPU(data *TelemetryData) {
	for i := 0; i < 4; i++ {
		base := fmt.Sprintf("/sys/class/drm/card%d/device/", i)

		vendor, err := os.ReadFile(base + "vendor")
		if err != nil {
			continue
		}

		switch strings.TrimSpace(string(vendor)) {

		case "0x8086": // Intel
			data.GPUName = "Intel GPU"
			data.ActiveDev = "Intel GPU"
			data.GPUUsage = readIntelEngineBusy(i)

			if vram, err := os.ReadFile(base + "lmem_total_bytes"); err == nil {
				val, _ := strconv.ParseUint(strings.TrimSpace(string(vram)), 10, 64)
				data.VRAMUsage = float64(val) / (1024 * 1024 * 1024)
			}
			return

		case "0x10de":
			data.GPUName = "NVIDIA GPU"
			data.ActiveDev = "NVIDIA GPU"
			data.GPUUsage = 0
			return

		case "0x1002":
			data.GPUName = "AMD GPU"
			data.ActiveDev = "AMD GPU"
			data.GPUUsage = 0
			return
		}
	}
}

/* Intel GPU usage via engine busy counters */
func readIntelEngineBusy(card int) int {
	enginePath := fmt.Sprintf("/sys/class/drm/card%d/engine", card)

	entries, err := os.ReadDir(enginePath)
	if err != nil {
		return 0
	}

	var busyNS uint64
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(enginePath, e.Name(), "busy"))
		if err != nil {
			continue
		}
		v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
		busyNS += v
	}

	now := time.Now()

	if !gpuInit {
		lastGPUBusy = busyNS
		lastGPUTime = now
		gpuInit = true
		return 0
	}

	dBusy := busyNS - lastGPUBusy
	dTime := now.Sub(lastGPUTime).Nanoseconds()

	lastGPUBusy = busyNS
	lastGPUTime = now

	if dTime <= 0 {
		return 0
	}

	usage := int((float64(dBusy) / float64(dTime)) * 100)
	if usage < 0 {
		return 0
	}
	if usage > 100 {
		return 100
	}
	return usage
}

/* ---------------- CPU ---------------- */

func readCPUUsage() float64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}

	fields := strings.Fields(strings.Split(string(b), "\n")[0])

	var idle, total uint64
	for i, f := range fields[1:] {
		n, _ := strconv.ParseUint(f, 10, 64)
		total += n
		if i == 3 {
			idle = n
		}
	}

	if !cpuInit {
		lastCPUIdle = idle
		lastCPUTotal = total
		cpuInit = true
		return 0
	}

	dIdle := idle - lastCPUIdle
	dTotal := total - lastCPUTotal

	lastCPUIdle = idle
	lastCPUTotal = total

	if dTotal == 0 {
		return 0
	}

	return 100 * float64(dTotal-dIdle) / float64(dTotal)
}

/* ---------------- RAM ---------------- */

func readRAMUsedGB() float64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}

	var total, avail uint64
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "MemTotal:") {
			fmt.Sscanf(l, "MemTotal: %d kB", &total)
		}
		if strings.HasPrefix(l, "MemAvailable:") {
			fmt.Sscanf(l, "MemAvailable: %d kB", &avail)
		}
	}
	return float64(total-avail) / (1024 * 1024)
}

/* ==================== OLLAMA ==================== */

func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
	var reqBody map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		http.Error(w, "invalid request", 400)
		return
	}

	reqBody["stream"] = true
	jsonData, _ := json.Marshal(reqBody)

	resp, err := h.client.Post(
		h.cfg.OllamaBaseURL+"/api/generate",
		"application/json",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]string{"error": "Ollama Unreachable"})
		return
	}
	defer resp.Body.Close()

	// Stream the response directly to the client
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		fmt.Fprintf(w, "%s\n", line)
		flusher.Flush()
	}
}

func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
	resp, err := h.client.Get(h.cfg.OllamaBaseURL + "/api/tags")
	if err != nil {
		http.Error(w, "offline", 502)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "no files uploaded", http.StatusBadRequest)
		return
	}

	// Load existing RAG index (if any)
	var chunks []RagChunk
	if existing, err := loadIndex(); err == nil {
		chunks = existing
	}

	// Save files + index .txt files
	for _, f := range files {
		dstPath := filepath.Join(h.cfg.UploadDir, filepath.Base(f.Filename))

		src, err := f.Open()
		if err != nil {
			continue
		}

		dst, err := os.Create(dstPath)
		if err != nil {
			src.Close()
			continue
		}

		_, _ = io.Copy(dst, src)
		src.Close()
		dst.Close()

		// ---- RAG indexing for text files ----
		lower := strings.ToLower(f.Filename)
		isTextFile := strings.HasSuffix(lower, ".txt") ||
			strings.HasSuffix(lower, ".md") ||
			strings.HasSuffix(lower, ".log") ||
			f.Filename == "LICENSE" ||
			f.Filename == "README"
		
		if isTextFile {
			data, err := os.ReadFile(dstPath)
			if err != nil {
				continue
			}

			for _, chunk := range chunkText(string(data)) {
				emb, err := embed(h.cfg, chunk)
				if err != nil {
					continue
				}

				chunks = append(chunks, RagChunk{
					Text:      chunk,
					Embedding: emb,
				})
			}
		}
	}

	_ = saveIndex(chunks)

	json.NewEncoder(w).Encode(map[string]any{
		"uploaded": len(files),
		"indexed":  len(chunks),
	})
}

// ReindexAll scans uploads directory and indexes all .txt files
func (h *Handlers) ReindexAll(w http.ResponseWriter, r *http.Request) {
	var chunks []RagChunk

	// Log the directory we're checking
	uploadDir := h.cfg.UploadDir
	
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		// Try alternate directory
		uploadDir = "uploads"
		entries, err = os.ReadDir(uploadDir)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "uploads directory not found in data/uploads or uploads/",
				"tried": h.cfg.UploadDir,
			})
			return
		}
	}

	indexed := 0
	filesList := []string{}
	txtFiles := []string{}
	errors := []string{}
	
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		filesList = append(filesList, filename)
		
		// Check if it's a text file - either .txt extension or common text files
		lower := strings.ToLower(filename)
		isTextFile := strings.HasSuffix(lower, ".txt") ||
			strings.HasSuffix(lower, ".md") ||
			strings.HasSuffix(lower, ".log") ||
			filename == "LICENSE" ||
			filename == "README" ||
			strings.HasPrefix(filename, ".bash")
		
		if !isTextFile {
			continue
		}

		txtFiles = append(txtFiles, filename)
		filePath := filepath.Join(uploadDir, filename)
		
		data, err := os.ReadFile(filePath)
		if err != nil {
			errors = append(errors, fmt.Sprintf("Read error %s: %v", filename, err))
			continue
		}

		textChunks := chunkText(string(data))
		errors = append(errors, fmt.Sprintf("File %s: %d bytes, %d chunks", filename, len(data), len(textChunks)))
		
		for i, chunk := range textChunks {
			emb, err := embed(h.cfg, chunk)
			if err != nil {
				errors = append(errors, fmt.Sprintf("Embed error chunk %d of %s: %v", i, filename, err))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]any{
					"error": "embedding failed - is nomic-embed-text model available?",
					"detail": err.Error(),
					"debug": errors,
				})
				return
			}

			chunks = append(chunks, RagChunk{
				Text:      chunk,
				Embedding: emb,
			})
		}
		indexed++
	}

	if err := saveIndex(chunks); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "failed to save index: " + err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"upload_dir":    uploadDir,
		"files_found":   filesList,
		"txt_files":     txtFiles,
		"files_indexed": indexed,
		"total_chunks":  len(chunks),
		"debug":         errors,
	})
}