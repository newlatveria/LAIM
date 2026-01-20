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
	"webolla/internal/config"
)

type Handler struct {
	cfg *config.Config
}

func New(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg}
}

// Index serves the frontend
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile("index.html")
	if err != nil {
		http.Error(w, "index.html not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(content)
}

// Models fetches available models from Ollama
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

// Generate handles the chat, RAG context, system instructions, and temperature options
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}

	// Updated struct to accept "Options" from the new UI
	var clientReq struct {
		Model    string                 `json:"model"`
		System   string                 `json:"system"`
		Options  map[string]interface{} `json:"options"` // e.g. { "temperature": 0.7 }
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	if err := json.NewDecoder(r.Body).Decode(&clientReq); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	// 1. Build File Context (RAG) from the uploads folder
	var fileContext string
	files, _ := os.ReadDir(h.cfg.UploadDir)
	for _, file := range files {
		if !file.IsDir() {
			data, err := os.ReadFile(filepath.Join(h.cfg.UploadDir, file.Name()))
			if err == nil {
				fileContext += fmt.Sprintf("\n--- FILE: %s ---\n%s\n", file.Name(), string(data))
			}
		}
	}

	// 2. Construct System Prompt (Instructions + Data)
	masterSystem := clientReq.System
	if masterSystem == "" {
		masterSystem = "You are a helpful AI assistant."
	}
	if fileContext != "" {
		masterSystem += "\n\nREFERENCE DATA:\n" + fileContext
	}

	// 3. Prepare Payload manually to ensure 'system' message is first
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	finalMessages := []msg{{Role: "system", Content: masterSystem}}
	for _, m := range clientReq.Messages {
		finalMessages = append(finalMessages, msg{Role: m.Role, Content: m.Content})
	}

	// 4. Forward to Ollama with Options
	ollamaReq := map[string]interface{}{
		"model":    clientReq.Model,
		"messages": finalMessages,
		"stream":   true,
		"options":  clientReq.Options, // This passes the temperature slider value
	}
	
	payloadBytes, _ := json.Marshal(ollamaReq)
	req, _ := http.NewRequest("POST", h.cfg.OllamaBaseURL+"/api/chat", bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Ollama connection failed", 502)
		return
	}
	defer resp.Body.Close()

	// 5. Stream Response back to browser
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", 500)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
		flusher.Flush()
	}
}

// Upload handles multiple files AND folder selection
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	// Parse up to 100MB
	if err := r.ParseMultipartForm(100 << 20); err != nil {
		http.Error(w, "Upload too large", 400)
		return
	}
	
	// Ensure directory exists
	if _, err := os.Stat(h.cfg.UploadDir); os.IsNotExist(err) {
		os.MkdirAll(h.cfg.UploadDir, 0755)
	}
	
	files := r.MultipartForm.File["files"]
	count := 0
	
	for _, fh := range files {
		src, err := fh.Open()
		if err != nil { continue }
		defer src.Close()

		// Save file to disk
		dstPath := filepath.Join(h.cfg.UploadDir, filepath.Base(fh.Filename))
		dst, err := os.Create(dstPath)
		if err != nil { continue }
		defer dst.Close()

		io.Copy(dst, src)
		count++
	}
	
	fmt.Fprintf(w, "Successfully uploaded %d files", count)
}

// ReindexAll clears the uploads directory
func (h *Handler) ReindexAll(w http.ResponseWriter, r *http.Request) {
	os.RemoveAll(h.cfg.UploadDir)
	os.MkdirAll(h.cfg.UploadDir, 0755)
	fmt.Fprint(w, "Context cleared")
}

// Stubs for remaining routes to prevent "undefined" errors
func (h *Handler) Rag(w http.ResponseWriter, r *http.Request)       { fmt.Fprint(w, "RAG active") }
func (h *Handler) Telemetry(w http.ResponseWriter, r *http.Request)  { fmt.Fprint(w, "{}") }