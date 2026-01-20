package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Configuration
const (
	defaultPort            = "8080"
	defaultOllamaBaseURL   = "http://localhost:11434"
	defaultChatTimeout     = 300 * time.Second
	defaultListTimeout     = 10 * time.Second
	defaultMaxRequestSize  = 50 * 1024 * 1024 // 50MB for file uploads
	defaultMaxFileSize     = 20 * 1024 * 1024 // 20MB per file
	defaultRateLimitPerSec = 10
	defaultRateLimitBurst  = 20
)

var (
	port           string
	ollamaBaseURL  string
	chatTimeout    time.Duration
	chatClient     *http.Client
	listClient     *http.Client
	limiter        *rate.Limiter
	requestTracker *RequestTracker
)

func init() {
	port = getEnv("PORT", defaultPort)
	ollamaBaseURL = getEnv("OLLAMA_BASE_URL", defaultOllamaBaseURL)
	timeoutSec, _ := strconv.Atoi(getEnv("CHAT_TIMEOUT_SEC", "300"))
	chatTimeout = time.Duration(timeoutSec) * time.Second

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	chatClient = &http.Client{
		Timeout:   chatTimeout,
		Transport: transport,
	}

	listClient = &http.Client{
		Timeout:   defaultListTimeout,
		Transport: transport,
	}

	rateLimit, _ := strconv.Atoi(getEnv("RATE_LIMIT_PER_SEC", "10"))
	rateBurst, _ := strconv.Atoi(getEnv("RATE_LIMIT_BURST", "20"))
	limiter = rate.NewLimiter(rate.Limit(rateLimit), rateBurst)

	requestTracker = NewRequestTracker()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Request Tracker
type RequestTracker struct {
	mu       sync.RWMutex
	requests map[string]context.CancelFunc
}

func NewRequestTracker() *RequestTracker {
	return &RequestTracker{
		requests: make(map[string]context.CancelFunc),
	}
}

func (rt *RequestTracker) Add(id string, cancel context.CancelFunc) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.requests[id] = cancel
}

func (rt *RequestTracker) Remove(id string) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	delete(rt.requests, id)
}

func (rt *RequestTracker) Cancel(id string) bool {
	rt.mu.RLock()
	cancel, exists := rt.requests[id]
	rt.mu.RUnlock()

	if exists {
		cancel()
		return true
	}
	return false
}

// Data structures
type Message struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"` // base64 encoded images
}

type GenerationParams struct {
	Temperature   float64 `json:"temperature"`
	TopP          float64 `json:"top_p"`
	TopK          int     `json:"top_k"`
	RepeatPenalty float64 `json:"repeat_penalty"`
	NumPredict    int     `json:"num_predict"`
}

type ChatRequest struct {
	Model    string           `json:"model"`
	Messages []Message        `json:"messages"`
	Params   GenerationParams `json:"params"`
}

type OllamaChatPayload struct {
	Model    string                 `json:"model"`
	Messages []Message              `json:"messages"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type OllamaResponseChunk struct {
	Model     string   `json:"model"`
	CreatedAt string   `json:"created_at"`
	Message   *Message `json:"message"`
	Done      bool     `json:"done"`
}

type OllamaModel struct {
	Name string `json:"name"`
}

type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

type ServerStatus struct {
	OllamaURL     string `json:"ollama_url"`
	Connected     bool   `json:"connected"`
	PortListening string `json:"port"`
}

type FileUploadResponse struct {
	Content  string `json:"content"`
	Filename string `json:"filename"`
	Type     string `json:"type"` // "text" or "image"
	Size     int64  `json:"size"`
}

// Validation
var modelNameRegex = regexp.MustCompile(`^[a-zA-Z0-9:_.-]+$`)

func validateModelName(name string) error {
	if len(name) == 0 || len(name) > 100 {
		return errors.New("model name must be between 1 and 100 characters")
	}
	if !modelNameRegex.MatchString(name) {
		return errors.New("model name contains invalid characters")
	}
	return nil
}

func validateMessages(messages []Message) error {
	if len(messages) == 0 {
		return errors.New("messages cannot be empty")
	}
	for i, msg := range messages {
		if msg.Role != "user" && msg.Role != "assistant" && msg.Role != "system" {
			return fmt.Errorf("invalid role at message %d: %s", i, msg.Role)
		}
		if len(msg.Content) == 0 && len(msg.Images) == 0 {
			return fmt.Errorf("message content and images cannot both be empty at index %d", i)
		}
		if len(msg.Content) > 500000 {
			return fmt.Errorf("message content too long at index %d", i)
		}
	}
	return nil
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

// File processing
func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".gif" || ext == ".webp"
}

func isTextFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	textExts := []string{".txt", ".md", ".json", ".xml", ".html", ".css", ".js", ".go", ".py", ".java", ".c", ".cpp", ".rs", ".yml", ".yaml", ".toml", ".ini", ".sh", ".bat"}
	for _, te := range textExts {
		if ext == te {
			return true
		}
	}
	return false
}

func processFile(fileHeader *http.Request, fieldName string) (*FileUploadResponse, error) {
	file, header, err := fileHeader.FormFile(fieldName)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if header.Size > defaultMaxFileSize {
		return nil, fmt.Errorf("file %s exceeds maximum size of %d bytes", header.Filename, defaultMaxFileSize)
	}

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("reading file %s: %w", header.Filename, err)
	}

	response := &FileUploadResponse{
		Filename: header.Filename,
		Size:     header.Size,
	}

	if isImageFile(header.Filename) {
		response.Type = "image"
		response.Content = base64.StdEncoding.EncodeToString(data)
	} else if isTextFile(header.Filename) {
		response.Type = "text"
		response.Content = string(data)
	} else {
		return nil, fmt.Errorf("unsupported file type: %s", header.Filename)
	}

	return response, nil
}

// Middleware
func rateLimitMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Request-ID")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next(w, r)
	}
}

func maxBytesMiddleware(maxSize int64) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxSize)
			next(w, r)
		}
	}
}

// Handlers
func main() {
	http.HandleFunc("/", serveHTML)
	http.HandleFunc("/api/chat", chain(handleChat, corsMiddleware, rateLimitMiddleware, maxBytesMiddleware(defaultMaxRequestSize)))
	http.HandleFunc("/api/upload", chain(handleFileUpload, corsMiddleware, rateLimitMiddleware, maxBytesMiddleware(defaultMaxRequestSize)))
	http.HandleFunc("/api/models", chain(handleListModels, corsMiddleware))
	http.HandleFunc("/api/status", chain(handleServerStatus, corsMiddleware))
	http.HandleFunc("/api/cancel", chain(handleCancelRequest, corsMiddleware))

	log.Printf("Chat server starting on http://localhost:%s", port)
	log.Printf("Ollama base URL: %s", ollamaBaseURL)
	log.Printf("Chat timeout: %v", chatTimeout)
	log.Printf("Max file size: %d MB", defaultMaxFileSize/(1024*1024))

	srv := &http.Server{
		Addr:         ":" + port,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: chatTimeout + 10*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())
}

func chain(f http.HandlerFunc, middlewares ...func(http.HandlerFunc) http.HandlerFunc) http.HandlerFunc {
	for i := len(middlewares) - 1; i >= 0; i-- {
		f = middlewares[i](f)
	}
	return f
}

func serveHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, htmlContent)
}

func handleServerStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := listClient.Get(ollamaBaseURL + "/api/tags")
	connected := err == nil && resp != nil && resp.StatusCode == http.StatusOK
	if resp != nil {
		resp.Body.Close()
	}

	status := ServerStatus{
		OllamaURL:     ollamaBaseURL,
		Connected:     connected,
		PortListening: port,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func handleCancelRequest(w http.ResponseWriter, r *http.Request) {
	requestID := r.URL.Query().Get("id")
	if requestID == "" {
		http.Error(w, "Missing request ID", http.StatusBadRequest)
		return
	}

	cancelled := requestTracker.Cancel(requestID)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"cancelled": cancelled,
	})
}

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(defaultMaxRequestSize); err != nil {
		http.Error(w, "Failed to parse multipart form: "+err.Error(), http.StatusBadRequest)
		return
	}

	var results []FileUploadResponse

	// Process all uploaded files
	if r.MultipartForm != nil && r.MultipartForm.File != nil {
		for fieldName := range r.MultipartForm.File {
			files := r.MultipartForm.File[fieldName]
			for i := range files {
				// Recreate request for each file
				fileReq := r
				result, err := processFile(fileReq, fieldName)
				if err != nil {
					log.Printf("Error processing file: %v", err)
					http.Error(w, "Error processing file: "+err.Error(), http.StatusBadRequest)
					return
				}
				results = append(results, *result)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(results)
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var chatReq ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&chatReq); err != nil {
		http.Error(w, "Invalid request payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := validateModelName(chatReq.Model); err != nil {
		http.Error(w, "Invalid model name: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := validateMessages(chatReq.Messages); err != nil {
		http.Error(w, "Invalid messages: "+err.Error(), http.StatusBadRequest)
		return
	}

	requestID := r.Header.Get("X-Request-ID")
	if requestID == "" {
		requestID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	ctx, cancel := context.WithTimeout(r.Context(), chatTimeout)
	defer cancel()

	requestTracker.Add(requestID, cancel)
	defer requestTracker.Remove(requestID)

	if err := streamChat(ctx, w, chatReq); err != nil {
		if errors.Is(err, context.Canceled) {
			log.Printf("Request %s cancelled", requestID)
			return
		}
		log.Printf("Chat error: %v", err)
		http.Error(w, "Chat failed: "+err.Error(), http.StatusInternalServerError)
	}
}

func streamChat(ctx context.Context, w http.ResponseWriter, chatReq ChatRequest) error {
	options := buildOptions(chatReq.Params)

	ollamaReq := OllamaChatPayload{
		Model:    chatReq.Model,
		Messages: chatReq.Messages,
		Stream:   true,
		Options:  options,
	}

	payloadBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		return fmt.Errorf("marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, ollamaBaseURL+"/api/chat", bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := chatClient.Do(req)
	if err != nil {
		return fmt.Errorf("connecting to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Ollama API error (%d): %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes)))
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming not supported")
	}

	scanner := bufio.NewScanner(resp.Body)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

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

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading stream: %w", err)
	}

	return nil
}

func handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp, err := listClient.Get(ollamaBaseURL + "/api/tags")
	if err != nil {
		log.Printf("Error connecting to Ollama: %v", err)
		http.Error(w, "Could not connect to Ollama", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		http.Error(w, fmt.Sprintf("Ollama error: %s", string(bodyBytes)), resp.StatusCode)
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

const htmlContent = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Ollama Chat</title>
    <script src="https://cdn.tailwindcss.com"></script>
    <style>
        .status-indicator { width: 12px; height: 12px; border-radius: 50%; display: inline-block; }
        .status-connected { background-color: #10b981; }
        .status-disconnected { background-color: #ef4444; }
        .chat-message { margin-bottom: 0.75rem; padding: 0.75rem 1rem; border-radius: 8px; max-width: 80%; word-wrap: break-word; }
        .chat-message.user { background-color: #e0e7ff; text-align: right; margin-left: auto; }
        .chat-message.assistant { background-color: #e5e7eb; text-align: left; margin-right: auto; }
        .chat-message img { max-width: 300px; max-height: 300px; border-radius: 4px; margin-top: 8px; }
        .slider-container { display: flex; align-items: center; gap: 12px; margin-bottom: 1rem; }
        .param-value { min-width: 50px; text-align: right; font-weight: 600; }
        .file-badge { display: inline-flex; align-items: center; gap: 4px; padding: 4px 8px; background: #dbeafe; border-radius: 4px; font-size: 12px; margin: 4px; }
        .file-badge button { background: #3b82f6; color: white; border: none; border-radius: 3px; padding: 2px 6px; cursor: pointer; }
        .file-badge button:hover { background: #2563eb; }
        #drop-zone { border: 2px dashed #cbd5e1; border-radius: 8px; padding: 20px; text-align: center; cursor: pointer; transition: all 0.2s; }
        #drop-zone.drag-over { border-color: #3b82f6; background-color: #eff6ff; }
    </style>
</head>
<body class="bg-gray-100 p-4">
    <div class="max-w-4xl mx-auto">
        <div class="bg-white rounded-lg shadow-md p-6 mb-6">
            <div class="flex justify-between items-center">
                <div>
                    <h1 class="text-4xl font-bold text-gray-900">Ollama Chat</h1>
                    <p class="text-gray-600">Chat with local LLMs + file context</p>
                </div>
                <div class="text-right">
                    <div class="text-sm text-gray-500 mb-2">Status</div>
                    <div class="flex items-center justify-end gap-2">
                        <span class="status-indicator status-disconnected" id="status-light"></span>
                        <span id="status-text" class="font-semibold text-red-600">Checking...</span>
                    </div>
                </div>
            </div>
        </div>

        <div class="bg-white rounded-lg shadow-md p-6 mb-6">
            <label class="block text-sm font-semibold text-gray-700 mb-3">Model:</label>
            <select id="model-select" class="w-full px-4 py-2 border rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500">
                <option value="">Loading...</option>
            </select>
        </div>

        <div class="bg-white rounded-lg shadow-md p-6 mb-6">
            <details class="cursor-pointer">
                <summary class="font-semibold text-gray-800 mb-4">⚙️ Parameters</summary>
                <div class="mt-4 space-y-3">
                    <div class="slider-container">
                        <label class="w-32 text-sm">Temperature:</label>
                        <input type="range" id="temp" class="flex-1" min="0" max="2" step="0.1" value="0.7">
                        <span id="temp-val" class="param-value">0.7</span>
                    </div>
                    <div class="slider-container">
                        <label class="w-32 text-sm">Top P:</label>
                        <input type="range" id="topp" class="flex-1" min="0" max="1" step="0.05" value="0.9">
                        <span id="topp-val" class="param-value">0.9</span>
                    </div>
                    <div class="slider-container">
                        <label class="w-32 text-sm">Max Tokens:</label>
                        <input type="range" id="maxtok" class="flex-1" min="50" max="4096" step="50" value="2048">
                        <span id="maxtok-val" class="param-value">2048</span>
                    </div>
                </div>
            </details>
        </div>

        <div class="bg-white rounded-lg shadow-md p-6 mb-6">
            <h3 class="font-semibold text-gray-800 mb-3">📎 Upload Files/Folders</h3>
            <div id="drop-zone" class="mb-4">
                <p class="text-gray-600 mb-2">Drop files here or click to browse</p>
                <p class="text-sm text-gray-500">Supports: Images (JPG, PNG, GIF), Text files (TXT, MD, JSON, Code)</p>
                <input type="file" id="file-input" multiple webkitdirectory directory class="hidden">
                <input type="file" id="file-input-single" multiple class="hidden">
            </div>
            <div class="flex gap-2 mb-4">
                <button id="upload-files-btn" class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700">📄 Select Files</button>
                <button id="upload-folder-btn" class="px-4 py-2 bg-green-600 text-white rounded-lg hover:bg-green-700">📁 Select Folder</button>
                <button id="clear-context-btn" class="px-4 py-2 bg-red-600 text-white rounded-lg hover:bg-red-700">🗑️ Clear All</button>
            </div>
            <div id="uploaded-files" class="flex flex-wrap gap-2"></div>
        </div>

        <div class="bg-white rounded-lg shadow-md p-6 mb-6">
            <div class="flex justify-between items-center mb-4">
                <h2 class="text-2xl font-bold">Chat</h2>
                <div class="flex gap-2">
                    <button id="clear-btn" class="px-4 py-2 bg-gray-500 text-white rounded-lg hover:bg-gray-600">Clear</button>
                    <button id="export-btn" class="px-4 py-2 bg-blue-500 text-white rounded-lg hover:bg-blue-600">Export</button>
                </div>
            </div>
            <div id="chat-history" class="bg-gray-50 border rounded-lg p-4 mb-4 h-96 overflow-y-auto"></div>
            <textarea id="chat-input" class="w-full px-4 py-2 border rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 mb-4" placeholder="Type your message..." rows="3"></textarea>
            <div class="flex gap-4">
                <button id="send-btn" class="flex-1 bg-indigo-600 text-white font-bold py-2 px-4 rounded-lg hover:bg-indigo-700">Send</button>
                <button id="cancel-btn" class="flex-1 bg-red-600 text-white font-bold py-2 px-4 rounded-lg hover:bg-red-700 hidden">Cancel</button>
            </div>
        </div>
    </div>

    <script>
        let messages = [];
        let currentRequestId = null;
        let uploadedContext = { texts: [], images: [] };

        const els = {
            modelSelect: document.getElementById('model-select'),
            chatHistory: document.getElementById('chat-history'),
            chatInput: document.getElementById('chat-input'),
            sendBtn: document.getElementById('send-btn'),
            cancelBtn: document.getElementById('cancel-btn'),
            clearBtn: document.getElementById('clear-btn'),
            exportBtn: document.getElementById('export-btn'),
            statusLight: document.getElementById('status-light'),
            statusText: document.getElementById('status-text'),
            temp: document.getElementById('temp'),
            tempVal: document.getElementById('temp-val'),
            topp: document.getElementById('topp'),
            toppVal: document.getElementById('topp-val'),
            maxtok: document.getElementById('maxtok'),
            maxtokVal: document.getElementById('maxtok-val'),
            dropZone: document.getElementById('drop-zone'),
            fileInput: document.getElementById('file-input'),
            fileInputSingle: document.getElementById('file-input-single'),
            uploadedFiles: document.getElementById('uploaded-files'),
            uploadFilesBtn: document.getElementById('upload-files-btn'),
            uploadFolderBtn: document.getElementById('upload-folder-btn'),
            clearContextBtn: document.getElementById('clear-context-btn'),
        };

        document.addEventListener('DOMContentLoaded', () => {
            fetchModels();
            checkStatus();
            setInterval(checkStatus, 5000);

            [
                { slider: els.temp, display: els.tempVal },
                { slider: els.topp, display: els.toppVal },
                { slider: els.maxtok, display: els.maxtokVal },
            ].forEach(({ slider, display }) => {
                slider.addEventListener('input', () => {
                    display.textContent = parseFloat(slider.value).toFixed(slider.step < 1 ? 2 : 0);
                });
            });

            els.sendBtn.addEventListener('click', sendMessage);
            els.cancelBtn.addEventListener('click', cancelRequest);
            els.clearBtn.addEventListener('click', () => {
                messages = [];
                els.chatHistory.innerHTML = '';
            });
            els.exportBtn.addEventListener('click', () => {
                const blob = new Blob([JSON.stringify(messages, null, 2)], { type: 'application/json' });
                const url = URL.createObjectURL(blob);
                const a = document.createElement('a');
                a.href = url;
                a.download = 'chat-history.json';
                a.click();
                URL.revokeObjectURL(url);
            });

            els.chatInput.addEventListener('keydown', (e) => {
                if (e.key === 'Enter' && !e.shiftKey) {
                    e.preventDefault();
                    sendMessage();
                }
            });

            // File upload handlers
            els.uploadFilesBtn.addEventListener('click', () => els.fileInputSingle.click());
            els.uploadFolderBtn.addEventListener('click', () => els.fileInput.click());
            els.clearContextBtn.addEventListener('click', clearContext);
            
            els.fileInputSingle.addEventListener('change', handleFileSelect);
            els.fileInput.addEventListener('change', handleFileSelect);

            // Drag and drop
            els.dropZone.addEventListener('click', () => els.fileInputSingle.click());
            els.dropZone.addEventListener('dragover', (e) => {
                e.preventDefault();
                els.dropZone.classList.add('drag-over');
            });
            els.dropZone.addEventListener('dragleave', () => {
                els.dropZone.classList.remove('drag-over');
            });
            els.dropZone.addEventListener('drop', (e) => {
                e.preventDefault();
                els.dropZone.classList.remove('drag-over');
                handleFiles(e.dataTransfer.files);
            });
        });

        async function checkStatus() {
            try {
                const res = await fetch('/api/status');
                const data = await res.json();
                if (data.connected) {
                    els.statusLight.className = 'status-indicator status-connected';
                    els.statusText.textContent = 'Connected';
                    els.statusText.className = 'font-semibold text-green-600';
                } else {
                    els.statusLight.className = 'status-indicator status-disconnected';
                    els.statusText.textContent = 'Disconnected';
                    els.statusText.className = 'font-semibold text-red-600';
                }
            } catch {
                els.statusLight.className = 'status-indicator status-disconnected';
                els.statusText.textContent = 'Error';
                els.statusText.className = 'font-semibold text-red-600';
            }
        }

        async function fetchModels() {
            try {
                const res = await fetch('/api/models');
                const data = await res.json();
                els.modelSelect.innerHTML = '';
                if (data.models && data.models.length > 0) {
                    data.models.forEach(m => {
                        const opt = document.createElement('option');
                        opt.value = m.name;
                        opt.textContent = m.name;
                        els.modelSelect.appendChild(opt);
                    });
                } else {
                    els.modelSelect.innerHTML = '<option>No models available</option>';
                }
            } catch (err) {
                showNotification('Failed to load models: ' + err.message, 'error');
            }
        }

        function handleFileSelect(e) {
            handleFiles(e.target.files);
        }

        async function handleFiles(fileList) {
            if (fileList.length === 0) return;

            const formData = new FormData();
            for (let i = 0; i < fileList.length; i++) {
                formData.append('files', fileList[i]);
            }

            try {
                const res = await fetch('/api/upload', {
                    method: 'POST',
                    body: formData
                });

                if (!res.ok) throw new Error(await res.text());

                const results = await res.json();
                results.forEach(file => {
                    if (file.type === 'text') {
                        uploadedContext.texts.push({ filename: file.filename, content: file.content, size: file.size });
                    } else if (file.type === 'image') {
                        uploadedContext.images.push({ filename: file.filename, content: file.content, size: file.size });
                    }
                });

                updateFileDisplay();
                showNotification(`Uploaded ${results.length} file(s)`, 'success');
            } catch (err) {
                showNotification('Upload failed: ' + err.message, 'error');
            }
        }

        function updateFileDisplay() {
            els.uploadedFiles.innerHTML = '';
            
            uploadedContext.texts.forEach((file, idx) => {
                const badge = document.createElement('div');
                badge.className = 'file-badge';
                badge.innerHTML = `
                    📄 ${file.filename} (${formatSize(file.size)})
                    <button onclick="removeFile('text', ${idx})">×</button>
                `;
                els.uploadedFiles.appendChild(badge);
            });

            uploadedContext.images.forEach((file, idx) => {
                const badge = document.createElement('div');
                badge.className = 'file-badge';
                badge.innerHTML = `
                    🖼️ ${file.filename} (${formatSize(file.size)})
                    <button onclick="removeFile('image', ${idx})">×</button>
                `;
                els.uploadedFiles.appendChild(badge);
            });
        }

        function removeFile(type, idx) {
            if (type === 'text') {
                uploadedContext.texts.splice(idx, 1);
            } else {
                uploadedContext.images.splice(idx, 1);
            }
            updateFileDisplay();
        }

        function clearContext() {
            uploadedContext = { texts: [], images: [] };
            updateFileDisplay();
            showNotification('Context cleared', 'success');
        }

        function formatSize(bytes) {
            if (bytes < 1024) return bytes + ' B';
            if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
            return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
        }

        async function sendMessage() {
            const content = els.chatInput.value.trim();
            const model = els.modelSelect.value;
            if (!content && uploadedContext.texts.length === 0 && uploadedContext.images.length === 0) {
                return showNotification('Please enter a message or upload files', 'error');
            }
            if (!model) return showNotification('Please select a model', 'error');

            // Build message with context
            let messageContent = content;
            if (uploadedContext.texts.length > 0) {
                messageContent = '=== FILE CONTEXT ===\n\n';
                uploadedContext.texts.forEach(file => {
                    messageContent += `--- ${file.filename} ---\n${file.content}\n\n`;
                });
                messageContent += '=== USER MESSAGE ===\n' + content;
            }

            const userMessage = {
                role: 'user',
                content: messageContent
            };

            // Add images if present
            if (uploadedContext.images.length > 0) {
                userMessage.images = uploadedContext.images.map(img => img.content);
            }

            messages.push(userMessage);
            appendMessage('user', content, uploadedContext.images.length > 0 ? uploadedContext.images : null);
            els.chatInput.value = '';

            els.sendBtn.classList.add('hidden');
            els.cancelBtn.classList.remove('hidden');

            currentRequestId = Date.now().toString();

            try {
                const res = await fetch('/api/chat', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'X-Request-ID': currentRequestId
                    },
                    body: JSON.stringify({
                        model,
                        messages,
                        params: {
                            temperature: parseFloat(els.temp.value),
                            top_p: parseFloat(els.topp.value),
                            num_predict: parseInt(els.maxtok.value),
                        }
                    }),
                });

                if (!res.ok) throw new Error(await res.text());

                const reader = res.body.getReader();
                const decoder = new TextDecoder();
                let buffer = '';
                let assistantMsg = '';
                const msgEl = document.createElement('div');
                msgEl.className = 'chat-message assistant';
                els.chatHistory.appendChild(msgEl);

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
                                if (json.message?.content) {
                                    assistantMsg += json.message.content;
                                    msgEl.textContent = assistantMsg;
                                    els.chatHistory.scrollTop = els.chatHistory.scrollHeight;
                                }
                            } catch (e) {}
                        }
                    }
                }

                if (assistantMsg) messages.push({ role: 'assistant', content: assistantMsg });
                showNotification('Message sent', 'success');
            } catch (err) {
                showNotification('Chat failed: ' + err.message, 'error');
            } finally {
                els.sendBtn.classList.remove('hidden');
                els.cancelBtn.classList.add('hidden');
                currentRequestId = null;
            }
        }

        async function cancelRequest() {
            if (currentRequestId) {
                try {
                    await fetch('/api/cancel?id=' + currentRequestId);
                    showNotification('Request cancelled', 'success');
                } catch (err) {
                    showNotification('Cancel failed', 'error');
                }
            }
        }

        function appendMessage(role, content, images = null) {
            const div = document.createElement('div');
            div.className = 'chat-message ' + role;
            div.textContent = content;
            
            if (images && images.length > 0) {
                images.forEach(img => {
                    const imgEl = document.createElement('img');
                    imgEl.src = 'data:image/jpeg;base64,' + img.content;
                    imgEl.alt = img.filename;
                    div.appendChild(imgEl);
                });
            }
            
            els.chatHistory.appendChild(div);
            els.chatHistory.scrollTop = els.chatHistory.scrollHeight;
        }

        function showNotification(msg, type) {
            const div = document.createElement('div');
            div.className = type === 'error' ? 'bg-red-100 border-l-4 border-red-500 text-red-700' : 'bg-green-100 border-l-4 border-green-500 text-green-700';
            div.className += ' p-4 fixed top-4 right-4 rounded shadow-lg z-50';
            div.textContent = msg;
            document.body.appendChild(div);
            setTimeout(() => div.remove(), 3000);
        }
    </script>
</body>
</html>`