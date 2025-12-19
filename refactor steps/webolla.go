package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Configuration constants and environment variables
const (
	defaultPort           = "8080"
	defaultOllamaBaseURL  = "http://localhost:11434"
	defaultGenerateTimeout = 300 * time.Second
	defaultListTimeout    = 10 * time.Second
)

var (
	port             string
	ollamaBaseURL    string
	generateTimeout  time.Duration
	ollamaGenerateAPI string
	ollamaChatAPI    string
	ollamaTagsAPI    string
	ollamaPullAPI    string
	ollamaDeleteAPI  string
)

func init() {
	port = getEnv("PORT", defaultPort)
	ollamaBaseURL = getEnv("OLLAMA_BASE_URL", defaultOllamaBaseURL)
	generateTimeoutSec, _ := strconv.Atoi(getEnv("GENERATE_TIMEOUT_SEC", "300"))
	generateTimeout = time.Duration(generateTimeoutSec) * time.Second

	ollamaGenerateAPI = ollamaBaseURL + "/api/generate"
	ollamaChatAPI = ollamaBaseURL + "/api/chat"
	ollamaTagsAPI = ollamaBaseURL + "/api/tags"
	ollamaPullAPI = ollamaBaseURL + "/api/pull"
	ollamaDeleteAPI = ollamaBaseURL + "/api/delete"
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Request/Response Structures
type GenerationParams struct {
	Temperature  float64 `json:"temperature"`
	TopP         float64 `json:"top_p"`
	TopK         int     `json:"top_k"`
	RepeatPenalty float64 `json:"repeat_penalty"`
	NumPredict   int     `json:"num_predict"`
}

type OllamaGenerateRequestPayload struct {
	Model  string            `json:"model"`
	Prompt string            `json:"prompt"`
	Stream bool              `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type OllamaChatRequestPayload struct {
	Model    string            `json:"model"`
	Messages []Message         `json:"messages"`
	Stream   bool              `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaModelActionPayload struct {
	Model string `json:"name"`
}

type OllamaResponseChunk struct {
	Model     string    `json:"model"`
	CreatedAt string    `json:"created_at"`
	Response  string    `json:"response"`
	Message   *Message  `json:"message"`
	Done      bool      `json:"done"`
}

type ClientRequest struct {
	ActionType string           `json:"actionType"`
	Model      string           `json:"model"`
	Prompt     string           `json:"prompt"`
	Messages   []Message        `json:"messages"`
	Params     GenerationParams `json:"params"`
}

type OllamaModel struct {
	Name string `json:"name"`
}

type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

type ServerStatus struct {
	OllamaURL    string `json:"ollama_url"`
	Connected    bool   `json:"connected"`
	PortListening string `json:"port"`
}

// Global HTTP client with connection pooling
var httpClient = &http.Client{
	Timeout: generateTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

func main() {
	http.HandleFunc("/", serveHTML)
	http.HandleFunc("/api/ollama-action", handleOllamaAction)
	http.HandleFunc("/api/models", handleListModels)
	http.HandleFunc("/api/status", handleServerStatus)
	http.HandleFunc("/api/cancel", handleCancelRequest)

	log.Printf("Server starting on http://localhost:%s", port)
	log.Printf("Ollama base URL: %s", ollamaBaseURL)
	log.Printf("Generate timeout: %v", generateTimeout)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func serveHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlContent)
}

func handleServerStatus(w http.ResponseWriter, r *http.Request) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ollamaTagsAPI)
	connected := err == nil && resp.StatusCode == http.StatusOK

	status := ServerStatus{
		OllamaURL:    ollamaBaseURL,
		Connected:    connected,
		PortListening: port,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func handleCancelRequest(w http.ResponseWriter, r *http.Request) {
	// This is a placeholder for request cancellation logic
	// In production, you'd track active requests and cancel them
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"status": "cancel signal received"}`)
}

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

	client := &http.Client{Timeout: generateTimeout, Transport: httpClient.Transport}

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
	options := buildOptions(clientReq.Params)
	
	ollamaReq := OllamaGenerateRequestPayload{
		Model:   clientReq.Model,
		Prompt:  clientReq.Prompt,
		Stream:  true,
		Options: options,
	}

	payloadBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "Error marshalling request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(http.MethodPost, ollamaGenerateAPI, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Error creating request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error connecting to Ollama: %v", err)
		http.Error(w, "Could not connect to Ollama at "+ollamaBaseURL, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Ollama API error: %d - %s", resp.StatusCode, string(bodyBytes))
		http.Error(w, fmt.Sprintf("Ollama error: %s", strings.TrimSpace(string(bodyBytes))), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("Streaming not supported")
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk OllamaResponseChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			log.Printf("Error unmarshalling response: %v", err)
			continue
		}

		if chunk.Response != "" {
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}

		if chunk.Done {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			break
		}
	}
}

func callChatAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	options := buildOptions(clientReq.Params)
	
	ollamaReq := OllamaChatRequestPayload{
		Model:    clientReq.Model,
		Messages: clientReq.Messages,
		Stream:   true,
		Options:  options,
	}

	payloadBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "Error marshalling request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(http.MethodPost, ollamaChatAPI, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Error creating request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error connecting to Ollama: %v", err)
		http.Error(w, "Could not connect to Ollama at "+ollamaBaseURL, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Ollama API error: %d - %s", resp.StatusCode, string(bodyBytes))
		http.Error(w, fmt.Sprintf("Ollama error: %s", strings.TrimSpace(string(bodyBytes))), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("Streaming not supported")
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk OllamaResponseChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			log.Printf("Error unmarshalling response: %v", err)
			continue
		}

		if chunk.Message != nil && chunk.Message.Content != "" {
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}

		if chunk.Done {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			break
		}
	}
}

func callModelPullAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	ollamaReq := OllamaModelActionPayload{Model: clientReq.Model}
	payloadBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "Error marshalling request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(http.MethodPost, ollamaPullAPI, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Error creating request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error connecting to Ollama: %v", err)
		http.Error(w, "Could not connect to Ollama", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Pull failed: %s", string(bodyBytes)), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write(bodyBytes)
}

func callModelDeleteAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	ollamaReq := OllamaModelActionPayload{Model: clientReq.Model}
	payloadBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "Error marshalling request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(http.MethodDelete, ollamaDeleteAPI, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Error creating request: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error connecting to Ollama: %v", err)
		http.Error(w, "Could not connect to Ollama", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		http.Error(w, fmt.Sprintf("Delete failed: %s", string(bodyBytes)), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write(bodyBytes)
}

func handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client := &http.Client{Timeout: defaultListTimeout, Transport: httpClient.Transport}
	resp, err := client.Get(ollamaTagsAPI)
	if err != nil {
		log.Printf("Error connecting to Ollama: %v", err)
		http.Error(w, "Could not connect to Ollama", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Error: %s", string(bodyBytes)), resp.StatusCode)
		return
	}

	var tagsResponse OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
		log.Printf("Error decoding response: %v", err)
		http.Error(w, "Error parsing models", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tagsResponse)
}

func buildOptions(params GenerationParams) map[string]interface{} {
	opts := make(map[string]interface{})
	if params.Temperature > 0 {
		opts["temperature"] = params.Temperature
	}
	if params.TopP > 0 {
		opts["top_p"] = params.TopP
	}
	if params.TopK > 0 {
		opts["top_k"] = params.TopK
	}
	if params.RepeatPenalty > 0 {
		opts["repeat_penalty"] = params.RepeatPenalty
	}
	if params.NumPredict > 0 {
		opts["num_predict"] = params.NumPredict
	}
	return opts
}

// Use the HTML from the separate HTML artifact - embed it here in production
const htmlContent = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Ollama Web UI - Enhanced</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <style>
        body { font-family: system-ui, -apple-system, sans-serif; background-color: #f3f4f6; }
        .container { max-width: 1000px; margin: 0 auto; }
        .status-indicator { width: 12px; height: 12px; border-radius: 50%; display: inline-block; }
        .status-connected { background-color: #10b981; }
        .status-disconnected { background-color: #ef4444; }
        .slider-container { display: flex; align-items: center; gap: 12px; margin-bottom: 1rem; }
        .slider { flex: 1; }
        .param-value { min-width: 50px; text-align: right; font-weight: 600; }
        .collapsible-header { cursor: pointer; user-select: none; display: flex; justify-content: space-between; align-items: center; padding: 1.5rem; background-color: #f9fafb; border-bottom: 1px solid #e5e7eb; }
        .collapsible-header:hover { background-color: #f3f4f6; }
        .collapsible-content { max-height: 0; overflow: hidden; transition: max-height 0.3s ease-out; }
        .collapsible-open .collapsible-content { max-height: 600px; }
        .chat-message { margin-bottom: 0.75rem; padding: 0.75rem 1rem; border-radius: 8px; max-width: 80%; word-wrap: break-word; }
        .chat-message.user { background-color: #e0e7ff; text-align: right; margin-left: auto; }
        .chat-message.assistant { background-color: #e5e7eb; text-align: left; margin-right: auto; }
        .response-toolbar { display: flex; gap: 8px; margin-top: 12px; flex-wrap: wrap; }
        .error-message { color: #dc2626; background-color: #fee2e2; padding: 12px; border-radius: 6px; border-left: 4px solid #dc2626; }
        .success-message { color: #059669; background-color: #d1fae5; padding: 12px; border-radius: 6px; border-left: 4px solid #059669; }
        .tab-buttons { display: flex; gap: 8px; margin-bottom: 16px; }
        .tab-button { padding: 8px 16px; border-radius: 6px; cursor: pointer; transition: all 0.2s; border: 2px solid transparent; }
        .tab-button.active { background-color: #4f46e5; color: white; }
        .tab-button:not(.active) { background-color: #e5e7eb; color: #374151; }
        .hidden { display: none; }
        .cancel-btn { background-color: #ef4444 !important; }
    </style>
</head>
<body class="bg-gray-100 p-4">
    <div class="container">
        <!-- Header -->
        <div class="bg-white rounded-lg shadow-md p-6 mb-6">
            <div class="flex justify-between items-center">
                <div>
                    <h1 class="text-4xl font-bold text-gray-900">Ollama Web UI</h1>
                    <p class="text-gray-600">Local LLM interaction with advanced controls</p>
                </div>
                <div class="text-right">
                    <div class="text-sm text-gray-500 mb-2">Server Status</div>
                    <div class="flex items-center justify-end gap-2">
                        <span class="status-indicator status-disconnected" id="status-light"></span>
                        <span id="status-text" class="font-semibold text-red-600">Checking...</span>
                    </div>
                </div>
            </div>
        </div>

        <!-- Tab Buttons -->
        <div class="mb-6">
            <div class="tab-buttons">
                <button class="tab-button active" data-tab="generate">Generate Text</button>
                <button class="tab-button" data-tab="chat">Chat</button>
                <button class="tab-button" data-tab="models">Model Management</button>
            </div>
        </div>

        <!-- Model Selection -->
        <div id="model-select-container" class="bg-white rounded-lg shadow-md p-6 mb-6">
            <label class="block text-sm font-semibold text-gray-700 mb-3">Select Model:</label>
            <select id="model-select" class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500">
                <option value="">Loading models...</option>
            </select>
        </div>

        <!-- Advanced Parameters -->
        <div class="bg-white rounded-lg shadow-md mb-6 collapsible-open">
            <div class="collapsible-header">
                <span class="font-semibold text-gray-800">⚙️ Advanced Parameters</span>
                <span class="text-gray-500">▼</span>
            </div>
            <div class="collapsible-content p-6">
                <div class="slider-container">
                    <label class="w-32 text-sm font-semibold text-gray-700">Temperature:</label>
                    <input type="range" id="temperature-slider" class="slider" min="0" max="2" step="0.1" value="0.7">
                    <span id="temperature-value" class="param-value">0.7</span>
                </div>
                <div class="slider-container">
                    <label class="w-32 text-sm font-semibold text-gray-700">Top P:</label>
                    <input type="range" id="top-p-slider" class="slider" min="0" max="1" step="0.05" value="0.9">
                    <span id="top-p-value" class="param-value">0.9</span>
                </div>
                <div class="slider-container">
                    <label class="w-32 text-sm font-semibold text-gray-700">Top K:</label>
                    <input type="range" id="top-k-slider" class="slider" min="0" max="100" step="1" value="40">
                    <span id="top-k-value" class="param-value">40</span>
                </div>
                <div class="slider-container">
                    <label class="w-32 text-sm font-semibold text-gray-700">Repeat Penalty:</label>
                    <input type="range" id="repeat-penalty-slider" class="slider" min="0" max="2" step="0.1" value="1.1">
                    <span id="repeat-penalty-value" class="param-value">1.1</span>
                </div>
                <div class="slider-container">
                    <label class="w-32 text-sm font-semibold text-gray-700">Max Tokens:</label>
                    <input type="range" id="max-tokens-slider" class="slider" min="50" max="4096" step="50" value="512">
                    <span id="max-tokens-value" class="param-value">512</span>
                </div>
            </div>
        </div>

        <!-- Generate Section -->
        <div id="generate-section" class="bg-white rounded-lg shadow-md p-6 mb-6">
            <h2 class="text-2xl font-bold text-gray-800 mb-4">Generate Text</h2>
            <textarea id="prompt-input" class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 mb-4" placeholder="Enter your prompt..." style="min-height: 120px;"></textarea>
            <div class="flex gap-4">
                <button id="generate-button" class="flex-1 bg-indigo-600 hover:bg-indigo-700 text-white font-bold py-2 px-4 rounded-lg transition">
                    Generate Response
                </button>
                <button id="generate-cancel-btn" class="cancel-btn flex-1 text-white font-bold py-2 px-4 rounded-lg transition hidden">
                    Cancel
                </button>
            </div>
        </div>

        <!-- Chat Section -->
        <div id="chat-section" class="hidden">
            <div class="bg-white rounded-lg shadow-md p-6 mb-6">
                <h2 class="text-2xl font-bold text-gray-800 mb-4">Chat with Model</h2>
                <div id="chat-history" class="bg-gray-50 border border-gray-200 rounded-lg p-4 mb-4" style="height: 400px; overflow-y: auto;"></div>
                <div class="mb-4 flex gap-4">
                    <button id="clear-chat-btn" class="bg-gray-500 hover:bg-gray-600 text-white font-bold py-2 px-4 rounded-lg transition">
                        Clear Chat
                    </button>
                    <button id="export-chat-btn" class="bg-blue-500 hover:bg-blue-600 text-white font-bold py-2 px-4 rounded-lg transition">
                        Export JSON
                    </button>
                </div>
                <textarea id="chat-input" class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 mb-4" placeholder="Type your message..." style="min-height: 100px;"></textarea>
                <div class="flex gap-4">
                    <button id="send-chat-btn" class="flex-1 bg-indigo-600 hover:bg-indigo-700 text-white font-bold py-2 px-4 rounded-lg transition">
                        Send Message
                    </button>
                    <button id="chat-cancel-btn" class="cancel-btn flex-1 text-white font-bold py-2 px-4 rounded-lg transition hidden">
                        Cancel
                    </button>
                </div>
            </div>
        </div>

        <!-- Model Management Section -->
        <div id="models-section" class="hidden">
            <div class="bg-white rounded-lg shadow-md p-6 mb-6">
                <h2 class="text-2xl font-bold text-gray-800 mb-4">Model Management</h2>
                
                <div class="mb-6 pb-6 border-b border-gray-200">
                    <h3 class="font-semibold text-gray-800 mb-3">Installed Models</h3>
                    <div class="flex gap-2 mb-4">
                        <select id="installed-models-select" class="flex-1 px-4 py-2 border border-gray-300 rounded-lg">
                            <option value="">Loading...</option>
                        </select>
                        <button id="refresh-models-btn" class="bg-blue-600 hover:bg-blue-700 text-white font-bold py-2 px-4 rounded-lg transition">
                            Refresh
                        </button>
                    </div>
                    <button id="delete-model-btn" class="w-full bg-red-600 hover:bg-red-700 text-white font-bold py-2 px-4 rounded-lg transition">
                        Delete Selected Model
                    </button>
                </div>

                <div>
                    <h3 class="font-semibold text-gray-800 mb-3">Pull Model</h3>
                    <input type="text" id="model-name-input" class="w-full px-4 py-2 border border-gray-300 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 mb-4" placeholder="e.g., llama2, mistral, phi">
                    <button id="pull-model-btn" class="w-full bg-green-600 hover:bg-green-700 text-white font-bold py-2 px-4 rounded-lg transition">
                        Pull Model
                    </button>
                </div>
            </div>

            <div id="model-action-output" class="bg-gray-50 border border-gray-200 rounded-lg p-6 font-mono text-sm text-gray-700 whitespace-pre-wrap hidden"></div>
        </div>

        <!-- Response Output -->
        <!-- System Status Panel -->
        <div id="system-status" class="bg-white rounded-lg shadow-md p-6 mb-6 hidden">
            <h3 class="text-lg font-semibold text-gray-800 mb-4">System Status</h3>
            <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
                <div class="bg-gray-50 p-4 rounded-lg">
                    <div class="text-sm text-gray-600">Status</div>
                    <div id="status-processing" class="text-lg font-bold text-indigo-600">Processing...</div>
                </div>
                <div class="bg-gray-50 p-4 rounded-lg">
                    <div class="text-sm text-gray-600">Device</div>
                    <div id="device-type" class="text-lg font-bold text-gray-800">Detecting...</div>
                </div>
                <div class="bg-gray-50 p-4 rounded-lg">
                    <div class="text-sm text-gray-600">Tokens/sec</div>
                    <div id="tokens-per-sec" class="text-lg font-bold text-gray-800">--</div>
                </div>
                <div class="bg-gray-50 p-4 rounded-lg">
                    <div class="text-sm text-gray-600">Load Time</div>
                    <div id="load-time" class="text-lg font-bold text-gray-800">--</div>
                </div>
            </div>
        </div>

        <!-- Thinking/Processing Panel -->
        <div id="thinking-panel" class="bg-white rounded-lg shadow-md p-6 mb-6 hidden">
            <div class="flex items-center justify-between mb-4">
                <h3 class="text-lg font-semibold text-gray-800">Thinking Process</h3>
                <label class="flex items-center gap-2 text-sm">
                    <input type="checkbox" id="show-thinking-checkbox" class="w-4 h-4">
                    <span>Show Details</span>
                </label>
            </div>
            <div id="thinking-output" class="bg-gray-50 border border-gray-300 rounded-lg p-4 font-mono text-sm text-gray-700 whitespace-pre-wrap max-h-48 overflow-y-auto"></div>
        </div>

        <div id="response-container" class="bg-white rounded-lg shadow-md p-6">
            <h2 class="text-2xl font-bold text-gray-800 mb-4">Response:</h2>
            <div id="response-output" class="min-h-24 text-gray-700 whitespace-pre-wrap"></div>
            <div id="response-toolbar" class="response-toolbar hidden">
                <button id="copy-response-btn" class="bg-indigo-600 hover:bg-indigo-700 text-white font-bold py-1 px-3 rounded transition">
                    Copy
                </button>
                <button id="export-response-btn" class="bg-blue-600 hover:bg-blue-600 text-white font-bold py-1 px-3 rounded transition">
                    Export
                </button>
                <button id="clear-response-btn" class="bg-gray-500 hover:bg-gray-600 text-white font-bold py-1 px-3 rounded transition">
                    Clear
                </button>
            </div>
        </div>
    </div>

    <script>
        const state = {
            currentTab: 'generate',
            chatMessages: [],
            isLoading: false,
            abortController: null,
        };

        const els = {
            tabButtons: document.querySelectorAll('.tab-button'),
            modelSelect: document.getElementById('model-select'),
            generateBtn: document.getElementById('generate-button'),
            generateCancelBtn: document.getElementById('generate-cancel-btn'),
            promptInput: document.getElementById('prompt-input'),
            responseOutput: document.getElementById('response-output'),
            responseToolbar: document.getElementById('response-toolbar'),
            statusLight: document.getElementById('status-light'),
            statusText: document.getElementById('status-text'),
            temperatureSlider: document.getElementById('temperature-slider'),
            temperatureValue: document.getElementById('temperature-value'),
            topPSlider: document.getElementById('top-p-slider'),
            topPValue: document.getElementById('top-p-value'),
            topKSlider: document.getElementById('top-k-slider'),
            topKValue: document.getElementById('top-k-value'),
            repeatPenaltySlider: document.getElementById('repeat-penalty-slider'),
            repeatPenaltyValue: document.getElementById('repeat-penalty-value'),
            maxTokensSlider: document.getElementById('max-tokens-slider'),
            maxTokensValue: document.getElementById('max-tokens-value'),
            chatHistory: document.getElementById('chat-history'),
            sendChatBtn: document.getElementById('send-chat-btn'),
            chatCancelBtn: document.getElementById('chat-cancel-btn'),
            chatInput: document.getElementById('chat-input'),
            clearChatBtn: document.getElementById('clear-chat-btn'),
            exportChatBtn: document.getElementById('export-chat-btn'),
            installedModelsSelect: document.getElementById('installed-models-select'),
            systemStatus: document.getElementById('system-status'),
            statusProcessing: document.getElementById('status-processing'),
            deviceType: document.getElementById('device-type'),
            tokensPerSec: document.getElementById('tokens-per-sec'),
            loadTime: document.getElementById('load-time'),
            thinkingPanel: document.getElementById('thinking-panel'),
            thinkingOutput: document.getElementById('thinking-output'),
            showThinkingCheckbox: document.getElementById('show-thinking-checkbox'),
        };

        document.addEventListener('DOMContentLoaded', () => {
            fetchModels();
            checkServerStatus();
            setupEventListeners();
            setupParameterSliders();
            setupTabButtons();
            setInterval(checkServerStatus, 5000);
        });

        function setupParameterSliders() {
            [
                { slider: els.temperatureSlider, display: els.temperatureValue },
                { slider: els.topPSlider, display: els.topPValue },
                { slider: els.topKSlider, display: els.topKValue },
                { slider: els.repeatPenaltySlider, display: els.repeatPenaltyValue },
                { slider: els.maxTokensSlider, display: els.maxTokensValue },
            ].forEach(({ slider, display }) => {
                slider.addEventListener('input', () => {
                    const val = parseFloat(slider.value);
                    const decimals = slider.step < 1 ? 2 : 0;
                    display.textContent = val.toFixed(decimals);
                });
            });
        }

        function setupTabButtons() {
            els.tabButtons.forEach(btn => {
                btn.addEventListener('click', () => {
                    const tabName = btn.dataset.tab;
                    state.currentTab = tabName;
                    
                    // Update active button
                    els.tabButtons.forEach(b => b.classList.remove('active'));
                    btn.classList.add('active');
                    
                    // Hide all sections
                    document.getElementById('generate-section').classList.add('hidden');
                    document.getElementById('chat-section').classList.add('hidden');
                    document.getElementById('models-section').classList.add('hidden');
                    
                    // Show selected section
                    if (tabName === 'generate') {
                        document.getElementById('generate-section').classList.remove('hidden');
                    } else if (tabName === 'chat') {
                        document.getElementById('chat-section').classList.remove('hidden');
                    } else if (tabName === 'models') {
                        document.getElementById('models-section').classList.remove('hidden');
                    }
                });
            });
        }

        async function checkServerStatus() {
            try {
                const response = await fetch('/api/status');
                const data = await response.json();
                if (data.connected) {
                    els.statusLight.classList.remove('status-disconnected');
                    els.statusLight.classList.add('status-connected');
                    els.statusText.textContent = 'Connected';
                    els.statusText.classList.remove('text-red-600');
                    els.statusText.classList.add('text-green-600');
                } else {
                    setDisconnected();
                }
            } catch (error) {
                setDisconnected();
            }
        }

        function setDisconnected() {
            els.statusLight.classList.remove('status-connected');
            els.statusLight.classList.add('status-disconnected');
            els.statusText.textContent = 'Disconnected';
            els.statusText.classList.remove('text-green-600');
            els.statusText.classList.add('text-red-600');
        }

        async function fetchModels() {
            try {
                const response = await fetch('/api/models');
                const data = await response.json();
                els.modelSelect.innerHTML = '';
                els.installedModelsSelect.innerHTML = '';
                
                if (data.models && data.models.length > 0) {
                    data.models.forEach(model => {
                        const option = document.createElement('option');
                        option.value = model.name;
                        option.textContent = model.name;
                        els.modelSelect.appendChild(option);
                        
                        const option2 = document.createElement('option');
                        option2.value = model.name;
                        option2.textContent = model.name;
                        els.installedModelsSelect.appendChild(option2);
                    });
                } else {
                    const option = document.createElement('option');
                    option.textContent = 'No models available';
                    els.modelSelect.appendChild(option);
                }
            } catch (error) {
                showError('Failed to load models: ' + error.message);
            }
        }

        function setupEventListeners() {
            els.generateBtn.addEventListener('click', handleGenerate);
            els.generateCancelBtn.addEventListener('click', handleCancel);
            els.sendChatBtn.addEventListener('click', handleSendChat);
            els.chatCancelBtn.addEventListener('click', handleCancel);
            els.clearChatBtn.addEventListener('click', () => {
                state.chatMessages = [];
                els.chatHistory.innerHTML = '';
            });
            els.exportChatBtn.addEventListener('click', exportChat);
            els.showThinkingCheckbox.addEventListener('change', () => {
                if (els.showThinkingCheckbox.checked) {
                    els.thinkingOutput.classList.remove('hidden');
                } else {
                    els.thinkingOutput.classList.add('hidden');
                }
            });
            document.getElementById('copy-response-btn').addEventListener('click', copyResponse);
            document.getElementById('export-response-btn').addEventListener('click', exportResponse);
            document.getElementById('clear-response-btn').addEventListener('click', () => {
                els.responseOutput.textContent = '';
                els.responseToolbar.classList.add('hidden');
            });
            document.getElementById('refresh-models-btn').addEventListener('click', fetchModels);
            document.getElementById('pull-model-btn').addEventListener('click', handlePullModel);
            document.getElementById('delete-model-btn').addEventListener('click', handleDeleteModel);
        }

        function getParams() {
            return {
                temperature: parseFloat(els.temperatureSlider.value),
                top_p: parseFloat(els.topPSlider.value),
                top_k: parseInt(els.topKSlider.value),
                repeat_penalty: parseFloat(els.repeatPenaltySlider.value),
                num_predict: parseInt(els.maxTokensSlider.value),
            };
        }

        async function handleGenerate() {
            const prompt = els.promptInput.value.trim();
            const model = els.modelSelect.value;
            if (!prompt) return showError('Please enter a prompt');
            if (!model) return showError('Please select a model');

            state.isLoading = true;
            els.generateBtn.classList.add('hidden');
            els.generateCancelBtn.classList.remove('hidden');
            els.responseOutput.textContent = '';
            els.systemStatus.classList.remove('hidden');
            els.thinkingPanel.classList.remove('hidden');
            els.thinkingOutput.textContent = '';

            const startTime = Date.now();
            let tokenCount = 0;
            let lastTokenTime = startTime;
            const generationStart = Date.now();

            try {
                els.statusProcessing.textContent = '⏳ Processing...';
                els.deviceType.textContent = 'Detecting...';
                els.tokensPerSec.textContent = '--';
                els.loadTime.textContent = '--';

                const response = await fetch('/api/ollama-action', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ actionType: 'generate', model, prompt, params: getParams() }),
                });

                if (!response.ok) throw new Error(await response.text());

                const reader = response.body.getReader();
                const decoder = new TextDecoder();
                let buffer = '';
                let firstTokenTime = null;

                while (true) {
                    const { done, value } = await reader.read();
                    if (done) break;
                    buffer += decoder.decode(value, { stream: true });
                    const lines = buffer.split('\n');
                    buffer = lines.pop();

                    for (const line of lines) {
                        if (line.startsWith('data: ')) {
                            const data = line.substring(6);
                            if (data === '[DONE]') continue;
                            try {
                                const json = JSON.parse(data);
                                if (json.response) {
                                    els.responseOutput.textContent += json.response;
                                    tokenCount++;

                                    if (!firstTokenTime) {
                                        firstTokenTime = Date.now();
                                        const loadTimeMs = firstTokenTime - startTime;
                                        els.loadTime.textContent = loadTimeMs + 'ms';
                                        els.statusProcessing.textContent = '✓ Generating';
                                    }

                                    const elapsedMs = Date.now() - lastTokenTime;
                                    if (elapsedMs >= 500) {
                                        const tokensPerSecond = (tokenCount / ((Date.now() - generationStart) / 1000)).toFixed(2);
                                        els.tokensPerSec.textContent = tokensPerSecond + ' tok/s';
                                        lastTokenTime = Date.now();
                                    }
                                }
                                
                                // Show thinking if available
                                if (json.model) {
                                    els.deviceType.textContent = json.model.split(':')[0];
                                }
                            } catch (e) {}
                        }
                    }
                }

                els.statusProcessing.textContent = '✓ Complete';
                const finalTokensPerSecond = (tokenCount / ((Date.now() - generationStart) / 1000)).toFixed(2);
                els.tokensPerSec.textContent = finalTokensPerSecond + ' tok/s';
                els.responseToolbar.classList.remove('hidden');
                showSuccess('Generation complete: ${tokenCount} tokens');
            } catch (error) {
                els.statusProcessing.textContent = '✗ Failed';
                showError('Generation failed: ' + error.message);
            } finally {
                state.isLoading = false;
                els.generateBtn.classList.remove('hidden');
                els.generateCancelBtn.classList.add('hidden');
            }
        }

        async function handleSendChat() {
            const message = els.chatInput.value.trim();
            const model = els.modelSelect.value;
            if (!message) return showError('Please enter a message');
            if (!model) return showError('Please select a model');

            state.chatMessages.push({ role: 'user', content: message });
            appendChatMessage('user', message);
            els.chatInput.value = '';

            state.isLoading = true;
            els.sendChatBtn.classList.add('hidden');
            els.chatCancelBtn.classList.remove('hidden');
            els.systemStatus.classList.remove('hidden');
            els.thinkingPanel.classList.remove('hidden');
            els.thinkingOutput.textContent = '';

            const startTime = Date.now();
            let tokenCount = 0;
            let lastTokenTime = startTime;
            const generationStart = Date.now();

            try {
                els.statusProcessing.textContent = '⏳ Processing...';
                els.deviceType.textContent = 'Detecting...';
                els.tokensPerSec.textContent = '--';
                els.loadTime.textContent = '--';

                const response = await fetch('/api/ollama-action', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ actionType: 'chat', model, messages: state.chatMessages, params: getParams() }),
                });

                if (!response.ok) throw new Error(await response.text());

                const reader = response.body.getReader();
                const decoder = new TextDecoder();
                let buffer = '', assistantResponse = '';
                const messageEl = document.createElement('div');
                messageEl.classList.add('chat-message', 'assistant');
                els.chatHistory.appendChild(messageEl);
                let firstTokenTime = null;

                while (true) {
                    const { done, value } = await reader.read();
                    if (done) break;
                    buffer += decoder.decode(value, { stream: true });
                    const lines = buffer.split('\n');
                    buffer = lines.pop();

                    for (const line of lines) {
                        if (line.startsWith('data: ')) {
                            const data = line.substring(6);
                            if (data === '[DONE]') continue;
                            try {
                                const json = JSON.parse(data);
                                if (json.message && json.message.content) {
                                    assistantResponse += json.message.content;
                                    messageEl.textContent = assistantResponse;
                                    els.chatHistory.scrollTop = els.chatHistory.scrollHeight;
                                    tokenCount++;

                                    if (!firstTokenTime) {
                                        firstTokenTime = Date.now();
                                        const loadTimeMs = firstTokenTime - startTime;
                                        els.loadTime.textContent = loadTimeMs + 'ms';
                                        els.statusProcessing.textContent = '✓ Generating';
                                    }

                                    const elapsedMs = Date.now() - lastTokenTime;
                                    if (elapsedMs >= 500) {
                                        const tokensPerSecond = (tokenCount / ((Date.now() - generationStart) / 1000)).toFixed(2);
                                        els.tokensPerSec.textContent = tokensPerSecond + ' tok/s';
                                        lastTokenTime = Date.now();
                                    }

                                    // Show thinking in panel if checkbox enabled
                                    if (els.showThinkingCheckbox.checked && json.message.content) {
                                        els.thinkingOutput.textContent += json.message.content;
                                        els.thinkingOutput.scrollTop = els.thinkingOutput.scrollHeight;
                                    }
                                }

                                if (json.model) {
                                    els.deviceType.textContent = json.model.split(':')[0];
                                }
                            } catch (e) {}
                        }
                    }
                }

                if (assistantResponse) state.chatMessages.push({ role: 'assistant', content: assistantResponse });
                els.statusProcessing.textContent = '✓ Complete';
                const finalTokensPerSecond = (tokenCount / ((Date.now() - generationStart) / 1000)).toFixed(2);
                els.tokensPerSec.textContent = finalTokensPerSecond + ' tok/s';
                showSuccess('Message sent: ${tokenCount} tokens');
            } catch (error) {
                els.statusProcessing.textContent = '✗ Failed';
                showError('Chat failed: ' + error.message);
            } finally {
                state.isLoading = false;
                els.sendChatBtn.classList.remove('hidden');
                els.chatCancelBtn.classList.add('hidden');
            }
        }

        function handleCancel() {
            state.isLoading = false;
            els.generateBtn.classList.remove('hidden');
            els.generateCancelBtn.classList.add('hidden');
            els.sendChatBtn.classList.remove('hidden');
            els.chatCancelBtn.classList.add('hidden');
            showSuccess('Cancelled');
        }

        function appendChatMessage(role, content) {
            const messageEl = document.createElement('div');
            messageEl.classList.add('chat-message', role);
            messageEl.textContent = content;
            els.chatHistory.appendChild(messageEl);
            els.chatHistory.scrollTop = els.chatHistory.scrollHeight;
        }

        function copyResponse() {
            navigator.clipboard.writeText(els.responseOutput.textContent);
            showSuccess('Copied to clipboard');
        }

        function exportResponse() {
            const blob = new Blob([els.responseOutput.textContent], { type: 'text/plain' });
            downloadFile(blob, 'response.txt');
        }

        function exportChat() {
            const blob = new Blob([JSON.stringify(state.chatMessages, null, 2)], { type: 'application/json' });
            downloadFile(blob, 'chat-history.json');
        }

        function downloadFile(blob, filename) {
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = filename;
            a.click();
            URL.revokeObjectURL(url);
        }

        async function handlePullModel() {
            const modelName = document.getElementById('model-name-input').value.trim();
            if (!modelName) return showError('Please enter a model name');

            try {
                const response = await fetch('/api/ollama-action', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ actionType: 'pull', model: modelName }),
                });

                if (!response.ok) throw new Error(await response.text());
                showSuccess('Pull initiated');
                fetchModels();
            } catch (error) {
                showError('Pull failed: ' + error.message);
            }
        }

        async function handleDeleteModel() {
            const model = els.installedModelsSelect.value;
            if (!model) return showError('Please select a model');
            if (!confirm('Delete ' + model + '? This cannot be undone.')) return;

            try {
                const response = await fetch('/api/ollama-action', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ actionType: 'delete', model }),
                });

                if (!response.ok) throw new Error(await response.text());
                showSuccess('Model deleted');
                fetchModels();
            } catch (error) {
                showError('Delete failed: ' + error.message);
            }
        }

        function showError(message) {
            const el = document.createElement('div');
            el.className = 'error-message';
            el.style.cssText = 'position: fixed; top: 20px; right: 20px; max-width: 400px; z-index: 1000;';
            el.textContent = message;
            document.body.appendChild(el);
            setTimeout(() => el.remove(), 5000);
        }

        function showSuccess(message) {
            const el = document.createElement('div');
            el.className = 'success-message';
            el.style.cssText = 'position: fixed; top: 20px; right: 20px; max-width: 400px; z-index: 1000;';
            el.textContent = message;
            document.body.appendChild(el);
            setTimeout(() => el.remove(), 3000);
        }
    </script>
</body>
</html>
`
