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

func (h *Handler) Index(w http.ResponseWriter, r *http.Request) {
	content, _ := os.ReadFile("index.html")
	w.Header().Set("Content-Type", "text/html")
	w.Write(content)
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

func (h *Handler) ListFiles(w http.ResponseWriter, r *http.Request) {
	files, _ := os.ReadDir(h.cfg.UploadDir)
	var names []string
	for _, f := range files {
		if !f.IsDir() {
			names = append(names, f.Name())
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(names)
}

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
	json.NewDecoder(r.Body).Decode(&clientReq)

	var fileContext string
	files, _ := os.ReadDir(h.cfg.UploadDir)
	for _, file := range files {
		data, _ := os.ReadFile(filepath.Join(h.cfg.UploadDir, file.Name()))
		fileContext += fmt.Sprintf("\n--- FILE: %s ---\n%s\n", file.Name(), string(data))
	}

	masterSystem := clientReq.System
	if fileContext != "" {
		masterSystem += "\n\nACT AS A KNOWLEDGE BASE ASSISTANT USING THESE FILES:\n" + fileContext
	}

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
	resp, _ := http.Post(h.cfg.OllamaBaseURL+"/api/chat", "application/json", bytes.NewBuffer(pBytes))
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
		flusher.Flush()
	}
}

func (h *Handler) Upload(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(100 << 20)
	os.MkdirAll(h.cfg.UploadDir, 0755)
	for _, fh := range r.MultipartForm.File["files"] {
		src, _ := fh.Open()
		dst, _ := os.Create(filepath.Join(h.cfg.UploadDir, filepath.Base(fh.Filename)))
		io.Copy(dst, src)
		src.Close(); dst.Close()
	}
}

func (h *Handler) PullModel(w http.ResponseWriter, r *http.Request) {
	var reqBody struct{ Name string `json:"name"` }
	json.NewDecoder(r.Body).Decode(&reqBody)
	payload, _ := json.Marshal(map[string]string{"name": reqBody.Name})
	resp, _ := http.Post(h.cfg.OllamaBaseURL+"/api/pull", "application/json", bytes.NewBuffer(payload))
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	flusher, _ := w.(http.Flusher)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
		flusher.Flush()
	}
}

func (h *Handler) DeleteModel(w http.ResponseWriter, r *http.Request) {
	var reqBody struct{ Name string `json:"name"` }
	json.NewDecoder(r.Body).Decode(&reqBody)
	payload, _ := json.Marshal(map[string]string{"model": reqBody.Name})
	req, _ := http.NewRequest(http.MethodDelete, h.cfg.OllamaBaseURL+"/api/delete", bytes.NewBuffer(payload))
	http.DefaultClient.Do(req)
}

func (h *Handler) ReindexAll(w http.ResponseWriter, r *http.Request) {
	os.RemoveAll(h.cfg.UploadDir); os.MkdirAll(h.cfg.UploadDir, 0755)
}

func (h *Handler) Rag(w http.ResponseWriter, r *http.Request) {}
func (h *Handler) Telemetry(w http.ResponseWriter, r *http.Request) {}