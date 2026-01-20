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

// Index serves the frontend index.html
func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	content, err := os.ReadFile("index.html")
	if err != nil {
		http.Error(w, "index.html not found", 404)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.Write(content)
}

// Models fetches the list of available Ollama models
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

// Generate reads uploaded files as context and streams the AI response
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}

	// 1. Decode Client Request
	var clientReq struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&clientReq); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}

	// 2. Build Context from Uploaded Files
	var contextContent string
	files, _ := os.ReadDir(h.cfg.UploadDir)
	for _, file := range files {
		if !file.IsDir() {
			data, err := os.ReadFile(filepath.Join(h.cfg.UploadDir, file.Name()))
			if err == nil {
				contextContent += fmt.Sprintf("\n--- FILE: %s ---\n%s\n", file.Name(), string(data))
			}
		}
	}

	// 3. Augment Prompt with Context
	if contextContent != "" && len(clientReq.Messages) > 0 {
		lastIdx := len(clientReq.Messages) - 1
		originalPrompt := clientReq.Messages[lastIdx].Content
		
		// This "System Prompt" wrapper forces the AI to look at your data
		clientReq.Messages[lastIdx].Content = fmt.Sprintf(
			"You are a helpful assistant. Use the provided context to answer the user request.\n\n[CONTEXT]%s\n\n[USER REQUEST]: %s",
			contextContent, originalPrompt,
		)
	}

	// 4. Forward to Ollama
	payload, _ := json.Marshal(clientReq)
	req, _ := http.NewRequest("POST", h.cfg.OllamaBaseURL+"/api/chat", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Ollama connection failed", 502)
		return
	}
	defer resp.Body.Close()

	// 5. Stream SSE Response
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", 500)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" { continue }
		fmt.Fprintf(w, "data: %s\n\n", line)
		flusher.Flush()
	}
}

// Upload saves files/folders to the configured uploads directory
func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", 405)
		return
	}

	// Max 50MB per upload batch
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, "Upload too large", 400)
		return
	}

	if _, err := os.Stat(h.cfg.UploadDir); os.IsNotExist(err) {
		os.MkdirAll(h.cfg.UploadDir, 0755)
	}

	files := r.MultipartForm.File["files"]
	for _, fh := range files {
		src, err := fh.Open()
		if err != nil { continue }
		defer src.Close()

		dstPath := filepath.Join(h.cfg.UploadDir, filepath.Base(fh.Filename))
		dst, err := os.Create(dstPath)
		if err != nil { continue }
		defer dst.Close()

		io.Copy(dst, src)
	}

	fmt.Fprint(w, "Upload successful")
}

// ReindexAll acts as our "Clear Context" button for now
func (h *Handler) ReindexAll(w http.ResponseWriter, r *http.Request) {
	err := os.RemoveAll(h.cfg.UploadDir)
	if err != nil {
		http.Error(w, "Failed to clear context", 500)
		return
	}
	os.MkdirAll(h.cfg.UploadDir, 0755)
	fmt.Fprint(w, "Context cleared successfully")
}

// Stubs for remaining routes
func (h *Handler) Rag(w http.ResponseWriter, r *http.Request)       { fmt.Fprint(w, "RAG is active on Generate") }
func (h *Handler) Telemetry(w http.ResponseWriter, r *http.Request)  { fmt.Fprint(w, "{}") }