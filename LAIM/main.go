package main

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

//go:embed static
var staticFiles embed.FS

// Base URL for the Ollama API
const ollamaBaseURL = "http://localhost:11434"
const ollamaGenerateAPI = ollamaBaseURL + "/api/generate"
const ollamaChatAPI = ollamaBaseURL + "/api/chat"
const ollamaTagsAPI = ollamaBaseURL + "/api/tags"
const ollamaPullAPI = ollamaBaseURL + "/api/pull"
const ollamaDeleteAPI = ollamaBaseURL + "/api/delete"

// --- API Request/Response Structures ---

type OllamaGenerateRequestPayload struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type OllamaChatRequestPayload struct {
	Model    string                 `json:"model"`
	Messages []Message              `json:"messages"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaModelActionPayload struct {
	Name string `json:"name"`
}

// ClientRequest structure now includes ContextFile
type ClientRequest struct {
	ActionType string                 `json:"actionType"`
	Model      string                 `json:"model"`
	Prompt     string                 `json:"prompt"`
	Messages   []Message              `json:"messages"`
	Options    map[string]interface{} `json:"options,omitempty"`
	ContextFile string                 `json:"contextFile,omitempty"` // NEW FIELD
}

type OllamaModel struct {
	Name string `json:"name"`
}

type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

// --- Helper: Get Local IP ---
func getOutboundIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "localhost" // Fallback
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}

// --- Main Server Logic ---

func main() {
	http.HandleFunc("/", serveRoot)
	http.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	http.HandleFunc("/api/ollama-action", handleOllamaAction)
	http.HandleFunc("/api/models", handleListModels)
	http.HandleFunc("/api/upload", handleUpload)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ip := getOutboundIP()
	log.Printf("---------------------------------------------------------")
	log.Printf("Server starting on:")
	log.Printf("  Local:   http://localhost:%s", port)
	log.Printf("  Network: http://%s:%s", ip, port)
	log.Printf("---------------------------------------------------------")
	log.Printf("Ensure Ollama is running at %s", ollamaBaseURL)
	
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func serveRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}
	content, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "Could not load UI", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// --- Upload Handler ---
func handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	r.ParseMultipartForm(100 << 20)

	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		http.Error(w, "No files received", http.StatusBadRequest)
		return
	}

	// Files are saved to the "./uploads" folder
	uploadDir := "./uploads"
	if _, err := os.Stat(uploadDir); os.IsNotExist(err) {
		os.Mkdir(uploadDir, 0755)
	}

	var savedFiles []string

	for _, handler := range files {
		dstPath := filepath.Join(uploadDir, handler.Filename)
		
		if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
			log.Printf("Error creating dir: %v", err)
			continue
		}

		dst, err := os.Create(dstPath)
		if err != nil {
			log.Printf("Error creating file: %v", err)
			continue
		}

		file, err := handler.Open()
		if err != nil {
			dst.Close()
			continue
		}

		if _, err := io.Copy(dst, file); err != nil {
			log.Printf("Error saving file: %v", err)
		}
		
		dst.Close()
		file.Close()
		savedFiles = append(savedFiles, handler.Filename)
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Successfully uploaded: %d files. Saved to: %s", len(savedFiles), uploadDir)
}

// --- Ollama Action Handlers ---

func handleOllamaAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var clientReq ClientRequest
	if err := json.NewDecoder(r.Body).Decode(&clientReq); err != nil {
		http.Error(w, "Invalid request payload: "+err.Error(), http.StatusBadRequest)
		return
	}
	client := &http.Client{Timeout: 300 * time.Second}
	switch clientReq.ActionType {
	case "generate":
		callGenerateAPI(w, r, clientReq, client)
	case "chat":
		callChatAPI(w, r, clientReq, client)
	case "pull":
		callModelPullAPI(w, r, clientReq, client)
	case "delete":
		callModelDeleteAPI(w, r, clientReq, client)
	default:
		http.Error(w, "Unknown action type", http.StatusBadRequest)
	}
}

// IMPORTANT: This is where we inject the file content into the prompt.
func callGenerateAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	finalPrompt := clientReq.Prompt
	
	if clientReq.ContextFile != "" {
		// Read the file from the uploads directory
		filePath := filepath.Join("uploads", clientReq.ContextFile)
		content, err := os.ReadFile(filePath)
		
		if err != nil {
			// File not found or couldn't be read
			log.Printf("Error reading context file %s: %v", filePath, err)
			http.Error(w, fmt.Sprintf("Error reading context file: %v", err), http.StatusBadRequest)
			return
		}

		// Inject the file content into the prompt with clear markers
		fileContent := string(content)
		contextPrefix := fmt.Sprintf("CONTEXT START: [File: %s]\n---\n%s\n---\nCONTEXT END.\n\n", clientReq.ContextFile, fileContent)
		finalPrompt = contextPrefix + finalPrompt
	}
	
	ollamaReq := OllamaGenerateRequestPayload{
		Model:   clientReq.Model,
		Prompt:  finalPrompt, // Use the prompt with injected context
		Stream:  true,
		Options: clientReq.Options,
	}
	proxyStreamRequest(w, ollamaGenerateAPI, ollamaReq, client)
}

func callChatAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	ollamaReq := OllamaChatRequestPayload{
		Model: clientReq.Model, Messages: clientReq.Messages, Stream: true, Options: clientReq.Options,
	}
	// Note: For chat, we would need to check ContextFile and insert it as the first message
	// or part of the first user message. For simplicity, we keep chat as-is for now.
	proxyStreamRequest(w, ollamaChatAPI, ollamaReq, client)
}

func callModelPullAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	proxyStandardRequest(w, ollamaPullAPI, OllamaModelActionPayload{Name: clientReq.Model}, client)
}

func callModelDeleteAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	payloadBytes, _ := json.Marshal(OllamaModelActionPayload{Name: clientReq.Model})
	req, _ := http.NewRequest(http.MethodDelete, ollamaDeleteAPI, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	handleStandardResponse(w, resp, err)
}

func handleListModels(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(ollamaTagsAPI)
	handleStandardResponse(w, resp, err)
}

func proxyStreamRequest(w http.ResponseWriter, apiUrl string, payload interface{}, client *http.Client) {
	// ... (rest of proxyStreamRequest is unchanged)
	payloadBytes, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, apiUrl, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Ollama Connection Error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "text/event-stream")
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
		if f, ok := w.(http.Flusher); ok { f.Flush() }
	}
}

func proxyStandardRequest(w http.ResponseWriter, url string, payload interface{}, client *http.Client) {
	payloadBytes, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	handleStandardResponse(w, resp, err)
}

func handleStandardResponse(w http.ResponseWriter, resp *http.Response, err error) {
	if err != nil {
		http.Error(w, "Error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}