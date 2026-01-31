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
	"sync"
	"webolla/internal/config"
)

// JOB MANAGER: Tracks background downloads
type DownloadJob struct {
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"` // 0-100
	Error    string  `json:"error,omitempty"`
}

var (
	jobMutex sync.RWMutex
	jobs     = make(map[string]*DownloadJob)
)

type Handler struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg}
}

// --- BASIC UI & TELEMETRY ---

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	content, _ := os.ReadFile("index.html")
	w.Header().Set("Content-Type", "text/html")
	w.Write(content)
}

func (h *Handler) Telemetry(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(h.cfg.OllamaBaseURL + "/api/ps")
	if err != nil {
		http.Error(w, "Ollama unreachable", 502)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (h *Handler) Models(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(h.cfg.OllamaBaseURL + "/api/tags")
	if err != nil {
		http.Error(w, "Ollama unreachable", 502)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

// --- CHAT & GENERATION (Context Aware) ---

func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	var clientReq struct {
		Model    string                 `json:"model"`
		System   string                 `json:"system"`
		Options  map[string]interface{} `json:"options"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&clientReq); err != nil {
		http.Error(w, "Bad JSON", 400)
		return
	}

	// 1. Inject RAG Context from Files
	var fileContext string
	files, _ := os.ReadDir(h.cfg.UploadDir)
	for _, file := range files {
		data, _ := os.ReadFile(filepath.Join(h.cfg.UploadDir, file.Name()))
		fileContext += fmt.Sprintf("\n--- FILE: %s ---\n%s\n", file.Name(), string(data))
	}

	masterSystem := clientReq.System
	if fileContext != "" {
		masterSystem += "\n\nCONTEXT FROM FILES:\n" + fileContext
	}

	// 2. Build Payload
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	finalPayload := []msg{{Role: "system", Content: masterSystem}}
	for _, m := range clientReq.Messages {
		finalPayload = append(finalPayload, msg{Role: m.Role, Content: m.Content})
	}

	ollamaReq := map[string]interface{}{
		"model":    clientReq.Model,
		"messages": finalPayload,
		"stream":   true,
		"options":  clientReq.Options,
	}
	
	pBytes, _ := json.Marshal(ollamaReq)
	
	// 3. Request with Context (Stop Button Support)
	req, _ := http.NewRequestWithContext(r.Context(), "POST", h.cfg.OllamaBaseURL+"/api/chat", bytes.NewBuffer(pBytes))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return // Client cancelled or error
	}
	defer resp.Body.Close()

	// 4. Stream Response
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
		flusher.Flush()
	}
}

// --- FILE MANAGEMENT ---

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(100 << 20) // 100MB limit
	os.MkdirAll(h.cfg.UploadDir, 0755)
	for _, fh := range r.MultipartForm.File["files"] {
		src, _ := fh.Open()
		dst, _ := os.Create(filepath.Join(h.cfg.UploadDir, filepath.Base(fh.Filename)))
		io.Copy(dst, src)
		src.Close(); dst.Close()
	}
}

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	files, _ := os.ReadDir(h.cfg.UploadDir)
	var names []string
	for _, f := range files {
		if !f.IsDir() { names = append(names, f.Name()) }
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(names)
}

func (h *Handler) ReindexAll(w http.ResponseWriter, r *http.Request) {
	os.RemoveAll(h.cfg.UploadDir)
	os.MkdirAll(h.cfg.UploadDir, 0755)
	w.Write([]byte("Files cleared"))
}

// --- MODEL MANAGEMENT (Queue & Editor) ---

func (h *Handler) GetJobs(w http.ResponseWriter, r *http.Request) {
	jobMutex.RLock()
	defer jobMutex.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(jobs)
}

func (h *Handler) PullModel(w http.ResponseWriter, r *http.Request) {
	var req struct { Names []string `json:"names"` }
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Println("Error decoding pull request:", err)
		return
	}

	go func() {
		for _, name := range req.Names {
			if name == "" { continue }
			fmt.Printf("Attempting to download: %s...\n", name) // Terminal Debug
			
			jobMutex.Lock()
			jobs[name] = &DownloadJob{Name: name, Status: "Connecting...", Progress: 0}
			jobMutex.Unlock()

			// Sending both "name" and "model" keys ensures compatibility
			payload, _ := json.Marshal(map[string]string{
				"name":  name,
				"model": name,
			})
			
			resp, err := http.Post(h.cfg.OllamaBaseURL+"/api/pull", "application/json", bytes.NewBuffer(payload))
			
			if err != nil {
				fmt.Printf("Failed to reach Ollama at %s: %v\n", h.cfg.OllamaBaseURL, err)
				jobMutex.Lock()
				jobs[name].Status = "Failed"
				jobs[name].Error = "Cannot reach Ollama server"
				jobMutex.Unlock()
				continue
			}
			
			// If Ollama returns 404/500, catch it here
			if resp.StatusCode != http.StatusOK {
				fmt.Printf("Ollama returned error %d for model %s\n", resp.StatusCode, name)
				jobMutex.Lock()
				jobs[name].Status = "Error"
				jobs[name].Error = fmt.Sprintf("Server returned %d", resp.StatusCode)
				jobMutex.Unlock()
				resp.Body.Close()
				continue
			}

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				var update struct {
					Status    string `json:"status"`
					Completed int64  `json:"completed"`
					Total     int64  `json:"total"`
				}
				if json.Unmarshal(scanner.Bytes(), &update) == nil {
					jobMutex.Lock()
					j := jobs[name]
					j.Status = update.Status
					if update.Total > 0 {
						j.Progress = (float64(update.Completed) / float64(update.Total)) * 100
					}
					if update.Status == "success" { 
						j.Progress = 100
						j.Status = "Installed" 
						fmt.Printf("Successfully installed %s\n", name)
					}
					jobMutex.Unlock()
				}
			}
			resp.Body.Close()
		}
	}()
	w.WriteHeader(http.StatusAccepted)
}

func (h *Handler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string `json:"name"` }
	json.NewDecoder(r.Body).Decode(&req)
	payload, _ := json.Marshal(map[string]string{"model": req.Name})
	request, _ := http.NewRequest(http.MethodDelete, h.cfg.OllamaBaseURL+"/api/delete", bytes.NewBuffer(payload))
	http.DefaultClient.Do(request)
}

func (h *Handler) GetModelfile(w http.ResponseWriter, r *http.Request) {
	var req struct{ Name string `json:"name"` }
	json.NewDecoder(r.Body).Decode(&req)
	payload, _ := json.Marshal(map[string]string{"name": req.Name})
	resp, _ := http.Post(h.cfg.OllamaBaseURL+"/api/show", "application/json", bytes.NewBuffer(payload))
	defer resp.Body.Close()
	io.Copy(w, resp.Body)
}

func (h *Handler) CreateModel(w http.ResponseWriter, r *http.Request) {
	resp, _ := http.Post(h.cfg.OllamaBaseURL+"/api/create", "application/json", r.Body)
	defer resp.Body.Close()
	io.Copy(w, resp.Body)
}