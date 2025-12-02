
package main

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// This directive tells Go to embed the "static" folder into the binary
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

type OllamaResponseChunk struct {
	Model    string   `json:"model"`
	Response string   `json:"response"` // For generate API
	Message  *Message `json:"message"`  // For chat API
	Done     bool     `json:"done"`
}

type ClientRequest struct {
	ActionType string                 `json:"actionType"` // "generate", "chat", "pull", "delete"
	Model      string                 `json:"model"`
	Prompt     string                 `json:"prompt"`   // For generate API
	Messages   []Message              `json:"messages"` // For chat API
	Options    map[string]interface{} `json:"options,omitempty"`
}

type OllamaModel struct {
	Name string `json:"name"`
}

type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

// --- Main Server Logic ---

func main() {
	// serveRoot handles the index.html
	http.HandleFunc("/", serveRoot)

	// This serves the static CSS and JS files
	// It automatically looks inside the embedded 'static' folder
	http.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	http.HandleFunc("/api/ollama-action", handleOllamaAction)
	http.HandleFunc("/api/models", handleListModels)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on http://localhost:%s", port)
	log.Printf("Make sure Ollama is running on %s", ollamaBaseURL)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func serveRoot(w http.ResponseWriter, r *http.Request) {
	// If the path isn't root (and hasn't been caught by /static/), return 404
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		http.NotFound(w, r)
		return
	}

	// Read the index.html from the embedded file system
	content, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "Could not load UI", http.StatusInternalServerError)
		log.Printf("Error reading index.html: %v", err)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

// handleOllamaAction is a unified handler for all Ollama API interactions.
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
		http.Error(w, "Unknown action type: "+clientReq.ActionType, http.StatusBadRequest)
	}
}

func callGenerateAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	ollamaReq := OllamaGenerateRequestPayload{
		Model:   clientReq.Model,
		Prompt:  clientReq.Prompt,
		Stream:  true,
		Options: clientReq.Options,
	}
	proxyStreamRequest(w, r, ollamaGenerateAPI, ollamaReq, client)
}

func callChatAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	ollamaReq := OllamaChatRequestPayload{
		Model:    clientReq.Model,
		Messages: clientReq.Messages,
		Stream:   true,
		Options:  clientReq.Options,
	}
	proxyStreamRequest(w, r, ollamaChatAPI, ollamaReq, client)
}

// Generic helper to handle streaming requests (Generate and Chat)
func proxyStreamRequest(w http.ResponseWriter, r *http.Request, apiUrl string, payload interface{}, client *http.Client) {
	payloadBytes, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, apiUrl, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "Ollama Connection Error: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		http.Error(w, "Ollama API Error: "+string(body), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	if f, ok := w.(http.Flusher); ok {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
			f.Flush()
		}
	}
}

func callModelPullAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	// Pull Logic
	proxyStandardRequest(w, ollamaPullAPI, OllamaModelActionPayload{Name: clientReq.Model}, client)
}

func callModelDeleteAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	// Delete Logic - Note: Ollama expects DELETE method usually, but here we proxy via POST or DELETE based on API needs.
	// We will stick to the standard logic used previously.
	payloadBytes, _ := json.Marshal(OllamaModelActionPayload{Name: clientReq.Model})
	req, _ := http.NewRequest(http.MethodDelete, ollamaDeleteAPI, bytes.NewBuffer(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := client.Do(req)
	handleStandardResponse(w, resp, err)
}

func handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(ollamaTagsAPI)
	handleStandardResponse(w, resp, err)
}

// Helper for non-streaming requests
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