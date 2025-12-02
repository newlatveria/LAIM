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
	"strings"
	"time"
)

// Base URL for the Ollama API
const ollamaBaseURL = "http://localhost:11434"
const ollamaGenerateAPI = ollamaBaseURL + "/api/generate"
const ollamaChatAPI = ollamaBaseURL + "/api/chat"
const ollamaTagsAPI = ollamaBaseURL + "/api/tags"
const ollamaPullAPI = ollamaBaseURL + "/api/pull"
const ollamaDeleteAPI = ollamaBaseURL + "/api/delete"

// --- API Request/Response Structures ---

// OllamaGenerateRequestPayload for /api/generate
type OllamaGenerateRequestPayload struct {
	Model   string                 `json:"model"`
	Prompt  string                 `json:"prompt"`
	Stream  bool                   `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

// OllamaChatRequestPayload for /api/chat
type OllamaChatRequestPayload struct {
	Model    string                 `json:"model"`
	Messages []Message              `json:"messages"`
	Stream   bool                   `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

// Message structure for chat API
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// OllamaModelActionPayload for /api/pull and /api/delete
type OllamaModelActionPayload struct {
	Name string `json:"name"` // Ollama uses 'name' for model actions
}

// OllamaResponseChunk for streaming responses (generate and chat)
type OllamaResponseChunk struct {
	Model     string   `json:"model"`
	CreatedAt string   `json:"created_at"`
	Response  string   `json:"response"` // For generate API
	Message   *Message `json:"message"`  // For chat API
	Done      bool     `json:"done"`
}

// ClientRequest from frontend to Go backend
type ClientRequest struct {
	ActionType string                 `json:"actionType"` // "generate", "chat", "pull", "delete"
	Model      string                 `json:"model"`
	Prompt     string                 `json:"prompt"`   // For generate API
	Messages   []Message              `json:"messages"` // For chat API
	Options    map[string]interface{} `json:"options,omitempty"`
}

// OllamaModel represents a single model returned by the /api/tags endpoint.
type OllamaModel struct {
	Name string `json:"name"`
}

// OllamaTagsResponse defines the structure of the JSON response from the /api/tags endpoint.
type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

// --- Main Server Logic ---

func main() {
	http.HandleFunc("/", serveHTML)
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

// serveHTML serves the main HTML page for the web UI.
func serveHTML(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Ollama Go Web UI</title>
    <script src="https://cdn.jsdelivr.net/npm/marked/marked.min.js"></script>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        body {
            font-family: 'Inter', sans-serif;
            transition: background-color 0.3s, color 0.3s;
        }
        body.light-mode {
            background-color: #f3f4f6;
            color: #111827;
        }
        body.dark-mode {
            background-color: #1f2937;
            color: #f9fafb;
        }
        .container {
            max-width: 800px;
            margin: 2rem auto;
            padding: 2rem;
            border-radius: 12px;
            box-shadow: 0 4px 12px rgba(0, 0, 0, 0.08);
            transition: background-color 0.3s;
        }
        body.light-mode .container {
            background-color: #ffffff;
        }
        body.dark-mode .container {
            background-color: #374151;
        }
        textarea, input, select {
            transition: background-color 0.3s, color 0.3s, border-color 0.3s;
        }
        body.dark-mode textarea,
        body.dark-mode input,
        body.dark-mode select {
            background-color: #4b5563;
            color: #f9fafb;
            border-color: #6b7280;
        }
        textarea {
            resize: vertical;
            min-height: 120px;
        }
        button {
            transition: background-color 0.2s ease-in-out, transform 0.1s ease-in-out;
        }
        button:hover:not(:disabled) {
            transform: translateY(-1px);
        }
        button:active:not(:disabled) {
            transform: translateY(0);
        }
        button:disabled {
            opacity: 0.5;
            cursor: not-allowed;
        }
        #loading-indicator {
            display: none;
            font-weight: 500;
            margin-top: 1rem;
        }
        body.light-mode #loading-indicator {
            color: #4f46e5;
        }
        body.dark-mode #loading-indicator {
            color: #818cf8;
        }
        #custom-alert-modal {
            z-index: 1000;
        }
        .chat-message {
            margin-bottom: 0.75rem;
            padding: 0.75rem 1rem;
            border-radius: 8px;
            max-width: 80%;
            word-wrap: break-word;
            position: relative;
        }
        .chat-message.user {
            margin-left: auto;
            text-align: right;
        }
        body.light-mode .chat-message.user {
            background-color: #e0e7ff;
        }
        body.dark-mode .chat-message.user {
            background-color: #4c1d95;
        }
        .chat-message.assistant {
            margin-right: auto;
            text-align: left;
        }
        body.light-mode .chat-message.assistant {
            background-color: #e5e7eb;
        }
        body.dark-mode .chat-message.assistant {
            background-color: #4b5563;
        }
        .chat-message pre {
            background-color: rgba(0,0,0,0.1);
            padding: 0.5rem;
            border-radius: 4px;
            overflow-x: auto;
            margin: 0.5rem 0;
        }
        .chat-message code {
            font-family: 'Courier New', monospace;
            font-size: 0.9em;
        }
        .chat-message p {
            margin: 0.5rem 0;
        }
        .chat-message ul, .chat-message ol {
            margin-left: 1.5rem;
        }
        .copy-button {
            position: absolute;
            top: 0.5rem;
            right: 0.5rem;
            padding: 0.25rem 0.5rem;
            font-size: 0.75rem;
            opacity: 0;
            transition: opacity 0.2s;
        }
        .chat-message:hover .copy-button {
            opacity: 1;
        }
        .api-section {
            border-radius: 8px;
            padding: 1.5rem;
            margin-bottom: 1.5rem;
            transition: background-color 0.3s, border-color 0.3s;
        }
        body.light-mode .api-section {
            border: 1px solid #e5e7eb;
            background-color: #f9fafb;
        }
        body.dark-mode .api-section {
            border: 1px solid #4b5563;
            background-color: #374151;
        }
        .api-section h2 {
            font-size: 1.25rem;
            font-weight: 600;
            margin-bottom: 1rem;
        }
        body.light-mode .api-section h2 {
            color: #374151;
        }
        body.dark-mode .api-section h2 {
            color: #f3f4f6;
        }
        #thinking-output {
            padding: 0.75rem;
            border-radius: 8px;
            margin-top: 1rem;
            font-style: italic;
            max-height: 100px;
            overflow-y: auto;
            word-wrap: break-word;
            transition: background-color 0.3s, border-color 0.3s, color 0.3s;
        }
        body.light-mode #thinking-output {
            background-color: #fffbeb;
            border: 1px dashed #fcd34d;
            color: #d97706;
        }
        body.dark-mode #thinking-output {
            background-color: #451a03;
            border: 1px dashed #f59e0b;
            color: #fbbf24;
        }
        .dark-mode-toggle {
            position: fixed;
            top: 1rem;
            right: 1rem;
            z-index: 100;
        }
        .slider-container {
            display: flex;
            align-items: center;
            gap: 1rem;
            margin-bottom: 1rem;
        }
        .slider {
            flex: 1;
        }
        body.dark-mode .modal-content {
            background-color: #374151;
            color: #f9fafb;
        }
        #response-output pre {
            background-color: rgba(0,0,0,0.1);
            padding: 0.75rem;
            border-radius: 4px;
            overflow-x: auto;
            margin: 0.5rem 0;
        }
        #response-output code {
            font-family: 'Courier New', monospace;
            font-size: 0.9em;
        }
        #response-output p {
            margin: 0.5rem 0;
        }
        #response-output ul, #response-output ol {
            margin-left: 1.5rem;
        }
    </style>
</head>
<body class="light-mode flex items-center justify-center min-h-screen p-4">
    <button id="dark-mode-toggle" class="dark-mode-toggle bg-gray-200 dark:bg-gray-700 hover:bg-gray-300 dark:hover:bg-gray-600 text-gray-800 dark:text-gray-200 font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-gray-500">
        🌙 Dark Mode
    </button>

    <div class="container w-full">
        <h1 class="text-4xl font-extrabold text-center mb-4">Ollama Go Web UI</h1>
        <p class="text-center text-gray-600 dark:text-gray-400 mb-8">Interact with your local Ollama instance for text generation, chat, and model management.</p>
        <p class="text-center text-gray-500 dark:text-gray-400 text-sm mb-8">Make sure Ollama is running on <code class="bg-gray-200 dark:bg-gray-700 px-1 py-0.5 rounded">http://localhost:11434</code> and you have downloaded models (e.g., <code class="bg-gray-200 dark:bg-gray-700 px-1 py-0.5 rounded">ollama pull llama2</code>).</p>

        <div class="mb-6">
            <label for="api-type-select" class="block text-sm font-medium mb-2">Select API Type:</label>
            <select id="api-type-select" class="shadow-sm appearance-none border rounded-lg w-full py-2 px-3 leading-tight focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent">
                <option value="generate">Generate Text</option>
                <option value="chat">Chat</option>
                <option value="model-management">Model Management</option>
            </select>
        </div>

        <div class="mb-6" id="common-model-select-container">
            <label for="model-select" class="block text-sm font-medium mb-2">Choose Ollama Model:</label>
            <select id="model-select" class="shadow-sm appearance-none border rounded-lg w-full py-2 px-3 leading-tight focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent">
                <option value="">Loading models...</option>
            </select>
        </div>

        <div class="mb-6" id="advanced-settings-container">
            <details class="api-section">
                <summary class="cursor-pointer font-semibold mb-4">⚙️ Advanced Settings</summary>
                
                <div class="slider-container">
                    <label for="temperature-slider" class="text-sm font-medium min-w-[120px]">Temperature:</label>
                    <input type="range" id="temperature-slider" class="slider" min="0" max="2" step="0.1" value="0.7">
                    <span id="temperature-value" class="text-sm font-mono min-w-[40px]">0.7</span>
                </div>

                <div class="slider-container">
                    <label for="top-p-slider" class="text-sm font-medium min-w-[120px]">Top P:</label>
                    <input type="range" id="top-p-slider" class="slider" min="0" max="1" step="0.05" value="0.9">
                    <span id="top-p-value" class="text-sm font-mono min-w-[40px]">0.9</span>
                </div>

                <div class="slider-container">
                    <label for="max-tokens-slider" class="text-sm font-medium min-w-[120px]">Max Tokens:</label>
                    <input type="range" id="max-tokens-slider" class="slider" min="128" max="4096" step="128" value="2048">
                    <span id="max-tokens-value" class="text-sm font-mono min-w-[40px]">2048</span>
                </div>
            </details>
        </div>

        <div id="generate-section" class="api-section">
            <h2>Generate Text</h2>
            <div class="mb-6">
                <label for="prompt-input" class="block text-sm font-medium mb-2">Prompt:</label>
                <textarea id="prompt-input" class="shadow-sm appearance-none border rounded-lg w-full py-2 px-3 leading-tight focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent" placeholder="Enter your prompt here..."></textarea>
            </div>
            <div class="flex gap-2">
                <button id="generate-button" class="flex-1 bg-indigo-600 hover:bg-indigo-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2">
                    Generate Response
                </button>
                <button id="stop-generate-button" class="hidden bg-red-600 hover:bg-red-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-2">
                    ⬛ Stop
                </button>
            </div>
        </div>

        <div id="chat-section" class="api-section hidden">
            <h2>Chat with Model</h2>
            
            <div class="mb-4">
                <label for="system-prompt-input" class="block text-sm font-medium mb-2">System Prompt (Optional):</label>
                <textarea id="system-prompt-input" class="shadow-sm appearance-none border rounded-lg w-full py-2 px-3 leading-tight focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent" rows="2" placeholder="You are a helpful assistant..."></textarea>
            </div>

            <div id="chat-history-output" class="bg-gray-50 dark:bg-gray-800 p-4 rounded-lg border border-gray-200 dark:border-gray-600 mb-4 h-64 overflow-y-auto flex flex-col space-y-2">
            </div>
            
            <div class="mb-4">
                <input type="checkbox" id="show-thinking-checkbox" class="mr-2">
                <label for="show-thinking-checkbox" class="text-sm font-medium">Display Thinking Process</label>
            </div>
            <div id="thinking-output" class="hidden text-sm mb-4">
            </div>
            <div class="mb-6">
                <label for="chat-input" class="block text-sm font-medium mb-2">Your Message:</label>
                <textarea id="chat-input" class="shadow-sm appearance-none border rounded-lg w-full py-2 px-3 leading-tight focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent" placeholder="Type your message..."></textarea>
            </div>
            <div class="flex gap-2 mb-4">
                <button id="send-chat-button" class="flex-1 bg-indigo-600 hover:bg-indigo-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2">
                    Send Message
                </button>
                <button id="stop-chat-button" class="hidden bg-red-600 hover:bg-red-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-2">
                    ⬛ Stop
                </button>
            </div>
            <div class="flex gap-2">
                <button id="export-chat-button" class="flex-1 bg-green-600 hover:bg-green-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-green-500 focus:ring-offset-2">
                    📥 Export Chat
                </button>
                <button id="clear-chat-button" class="flex-1 bg-gray-600 hover:bg-gray-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-gray-500 focus:ring-offset-2">
                    🗑️ Clear Chat
                </button>
            </div>
        </div>

        <div id="model-management-section" class="api-section hidden">
            <h2>Model Management</h2>
            <div class="mb-4">
                <label for="model-action-select" class="block text-sm font-medium mb-2">Select Installed Model for Action:</label>
                <select id="model-action-select" class="shadow-sm appearance-none border rounded-lg w-full py-2 px-3 leading-tight focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent">
                    <option value="">No models loaded</option>
                </select>
                <button id="refresh-models-button" class="mt-2 w-full bg-blue-600 hover:bg-blue-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500 focus:ring-offset-2">
                    Refresh Installed Models List
                </button>
            </div>

            <div class="mb-4">
                <label for="available-model-select" class="block text-sm font-medium mb-2">Select Model to Install (from Ollama Registry):</label>
                <select id="available-model-select" class="shadow-sm appearance-none border rounded-lg w-full py-2 px-3 leading-tight focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent">
                    <option value="">Loading available models...</option>
                </select>
                <div id="available-model-description" class="mt-2 p-3 bg-gray-100 dark:bg-gray-700 border border-gray-200 dark:border-gray-600 rounded-lg text-sm hidden"></div>
                <button id="pull-available-model-button" class="mt-2 w-full bg-green-600 hover:bg-green-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-green-500 focus:ring-offset-2">
                    Pull Selected Model
                </button>
            </div>

            <div class="mb-4">
                <label for="model-action-input" class="block text-sm font-medium mb-2">Or, Enter Model Name Manually (for Pull/Delete):</label>
                <input type="text" id="model-action-input" class="shadow-sm appearance-none border rounded-lg w-full py-2 px-3 leading-tight focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-transparent" placeholder="e.g., llama2:latest or mistral:7b">
            </div>
            <div class="flex space-x-4">
                <button id="pull-manual-model-button" class="flex-1 bg-green-600 hover:bg-green-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-green-500 focus:ring-offset-2">
                    Pull Manual Model
                </button>
                <button id="delete-model-button" class="flex-1 bg-red-600 hover:bg-red-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-red-500 focus:ring-offset-2">
                    Delete Model
                </button>
            </div>
            <div id="model-action-output" class="mt-4 bg-gray-50 dark:bg-gray-800 p-4 rounded-lg border border-gray-200 dark:border-gray-600 whitespace-pre-wrap text-base"></div>
        </div>

        <div id="loading-indicator" class="text-center mt-4 font-semibold">
            Generating... Please wait.
        </div>

        <div id="unified-response-output" class="mt-8 bg-gray-50 dark:bg-gray-800 p-6 rounded-lg border border-gray-200 dark:border-gray-600">
            <div class="flex justify-between items-center mb-4">
                <h2 class="text-xl font-semibold">Response:</h2>
                <button id="copy-response-button" class="bg-gray-600 hover:bg-gray-700 text-white font-bold py-1 px-3 rounded-lg text-sm focus:outline-none focus:ring-2 focus:ring-gray-500">
                    📋 Copy
                </button>
            </div>
            <div id="response-output" class="whitespace-pre-wrap text-base"></div>
        </div>
    </div>

    <div id="custom-alert-modal" class="fixed inset-0 bg-gray-600 bg-opacity-50 flex items-center justify-center hidden">
        <div class="modal-content bg-white p-6 rounded-lg shadow-xl max-w-sm w-full">
            <h3 id="custom-alert-title" class="text-lg font-semibold mb-4">Alert</h3>
            <p id="custom-alert-message" class="mb-6"></p>
            <div class="flex justify-end space-x-4">
                <button id="custom-alert-cancel" class="hidden bg-gray-300 hover:bg-gray-400 text-gray-800 font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-gray-500 focus:ring-offset-2">
                    Cancel
                </button>
                <button id="custom-alert-ok" class="bg-indigo-600 hover:bg-indigo-700 text-white font-bold py-2 px-4 rounded-lg focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2">
                    OK
                </button>
            </div>
        </div>
    </div>

    <script>
        // Dark mode toggle
        const darkModeToggle = document.getElementById('dark-mode-toggle');
        const body = document.body;
        
        // Load dark mode preference
        const darkMode = localStorage.getItem('darkMode') === 'true';
        if (darkMode) {
            body.classList.remove('light-mode');
            body.classList.add('dark-mode');
            darkModeToggle.textContent = '☀️ Light Mode';
        }

        darkModeToggle.addEventListener('click', () => {
            body.classList.toggle('dark-mode');
            body.classList.toggle('light-mode');
            const isDark = body.classList.contains('dark-mode');
            darkModeToggle.textContent = isDark ? '☀️ Light Mode' : '🌙 Dark Mode';
            localStorage.setItem('darkMode', isDark);
        });

        // Advanced settings sliders
        const temperatureSlider = document.getElementById('temperature-slider');
        const temperatureValue = document.getElementById('temperature-value');
        const topPSlider = document.getElementById('top-p-slider');
        const topPValue = document.getElementById('top-p-value');
        const maxTokensSlider = document.getElementById('max-tokens-slider');
        const maxTokensValue = document.getElementById('max-tokens-value');

        temperatureSlider.addEventListener('input', (e) => {
            temperatureValue.textContent = e.target.value;
        });
        topPSlider.addEventListener('input', (e) => {
            topPValue.textContent = e.target.value;
        });
        maxTokensSlider.addEventListener('input', (e) => {
            maxTokensValue.textContent = e.target.value;
        });

        function getOptions() {
            return {
                temperature: parseFloat(temperatureSlider.value),
                top_p: parseFloat(topPSlider.value),
                num_predict: parseInt(maxTokensSlider.value)
            };
        }

        const apiTypeSelect = document.getElementById('api-type-select');
        const modelSelect = document.getElementById('model-select');
        const promptInput = document.getElementById('prompt-input');
        const generateButton = document.getElementById('generate-button');
        const stopGenerateButton = document.getElementById('stop-generate-button');
        const responseOutput = document.getElementById('response-output');
        const copyResponseButton = document.getElementById('copy-response-button');
        const loadingIndicator = document.getElementById('loading-indicator');

        const generateSection = document.getElementById('generate-section');
        const chatSection = document.getElementById('chat-section');
        const modelManagementSection = document.getElementById('model-management-section');

        const systemPromptInput = document.getElementById('system-prompt-input');
        const chatInput = document.getElementById('chat-input');
        const sendChatButton = document.getElementById('send-chat-button');
        const stopChatButton = document.getElementById('stop-chat-button');
        const exportChatButton = document.getElementById('export-chat-button');
        const clearChatButton = document.getElementById('clear-chat-button');
        const chatHistoryOutput = document.getElementById('chat-history-output');
        const showThinkingCheckbox = document.getElementById('show-thinking-checkbox');
        const thinkingOutput = document.getElementById('thinking-output');

        const modelActionSelect = document.getElementById('model-action-select');
        const refreshModelsButton = document.getElementById('refresh-models-button');
        const availableModelSelect = document.getElementById('available-model-select');
        const availableModelDescription = document.getElementById('available-model-description');
        const pullAvailableModelButton = document.getElementById('pull-available-model-button');
        const modelActionInput = document.getElementById('model-action-input');
        const pullManualModelButton = document.getElementById('pull-manual-model-button');
        const deleteModelButton = document.getElementById('delete-model-button');
        const modelActionOutput = document.getElementById('model-action-output');
        const unifiedResponseOutput = document.getElementById('unified-response-output');
        const commonModelSelectContainer = document.getElementById('common-model-select-container');
        const advancedSettingsContainer = document.getElementById('advanced-settings-container');

        const customAlertModal = document.getElementById('custom-alert-modal');
        const customAlertTitle = document.getElementById('custom-alert-title');
        const customAlertMessage = document.getElementById('custom-alert-message');
        const customAlertOkButton = document.getElementById('custom-alert-ok');
        const customAlertCancelButton = document.getElementById('custom-alert-cancel');

        let resolveAlertPromise;
        let currentReader = null; // For stopping generation

        function showAlert(message, title = "Alert") {
            customAlertTitle.textContent = title;
            customAlertMessage.textContent = message;
            customAlertCancelButton.classList.add('hidden');
            customAlertOkButton.textContent = 'OK';
            customAlertModal.classList.remove('hidden');
            return new Promise(resolve => {
                resolveAlertPromise = resolve;
            });
        }

        function showConfirm(message, title = "Confirm") {
            customAlertTitle.textContent = title;
            customAlertMessage.textContent = message;
            customAlertCancelButton.classList.remove('hidden');
            customAlertOkButton.textContent = 'Confirm';
            customAlertModal.classList.remove('hidden');
            return new Promise(resolve => {
                resolveAlertPromise = resolve;
            });
        }

        customAlertOkButton.addEventListener('click', () => {
            customAlertModal.classList.add('hidden');
            if (resolveAlertPromise) {
                resolveAlertPromise(true);
            }
        });

        customAlertCancelButton.addEventListener('click', () => {
            customAlertModal.classList.add('hidden');
            if (resolveAlertPromise) {
                resolveAlertPromise(false);
            }
        });

        let chatMessages = [];

        // Copy response button
        copyResponseButton.addEventListener('click', () => {
            const text = responseOutput.textContent;
            navigator.clipboard.writeText(text).then(() => {
                const originalText = copyResponseButton.textContent;
                copyResponseButton.textContent = '✅ Copied!';
                setTimeout(() => {
                    copyResponseButton.textContent = originalText;
                }, 2000);
            });
        });

        // Export chat functionality - FIX APPLIED HERE
        exportChatButton.addEventListener('click', () => {
            if (chatMessages.length === 0) {
                showAlert('No chat history to export.');
                return;
            }

            const systemPrompt = systemPromptInput.value.trim();
            let exportContent = '# Ollama Chat Export\n\n';
            // Corrected template literal usage
            exportContent += '**Model:** ' + modelSelect.value + '\n';
            exportContent += '**Date:** ' + new Date().toLocaleString() + '\n\n';
            
            if (systemPrompt) {
                exportContent += '**System Prompt:** ' + systemPrompt + '\n\n';
            }
            
            exportContent += '---\n\n';

            chatMessages.forEach(msg => {
                // Corrected template literal usage
                exportContent += '### ' + (msg.role === 'user' ? 'User' : 'Assistant') + '\n\n';
                exportContent += msg.content + '\n\n';
            });

            const blob = new Blob([exportContent], { type: 'text/markdown' });
            const url = URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            // Corrected template literal usage
            a.download = 'ollama-chat-' + new Date().getTime() + '.md';
            document.body.appendChild(a);
            a.click();
            document.body.removeChild(a);
            URL.revokeObjectURL(url);
        });

        // Clear chat functionality
        clearChatButton.addEventListener('click', async () => {
            if (chatMessages.length === 0) {
                return;
            }
            const confirmed = await showConfirm('Are you sure you want to clear the chat history?');
            if (confirmed) {
                chatMessages = [];
                chatHistoryOutput.innerHTML = '';
            }
        });

        // Hardcoded list of common Ollama models with descriptions
        const availableModels = [
            { name: "llama2", description: "A powerful open-source large language model from Meta." },
            { name: "mistral", description: "A small, yet powerful, language model from Mistral AI, optimized for performance." },
            { name: "gemma", description: "Lightweight, state-of-the-art open models from Google, built from the same research and technology used to create the Gemini models." },
            { name: "phi", description: "A small language model from Microsoft, ideal for research and experimentation." },
            { name: "codellama", description: "A family of large language models from Meta designed for code generation and understanding." },
            { name: "neural-chat", description: "Fine-tuned for engaging conversational AI experiences." },
            { name: "dolphin-phi", description: "A fine-tuned version of Phi-2, designed for helpful and harmless chat." },
            { name: "openhermes", description: "A powerful model trained on a diverse range of datasets for general conversational tasks." },
            { name: "tinyllama", description: "A compact language model, great for resource-constrained environments or quick experiments." },
            { name: "vicuna", description: "A chatbot trained by fine-tuning LLaMA on user-shared conversations." },
            { name: "wizardlm", description: "An instruction-following LLM, based on LLaMA, fine-tuned with a large amount of instruction data." },
            { name: "zephyr", description: "A series of language models that are fine-tuned versions of Mistral, optimized for helpfulness." },
            { name: "stable-beluga", description: "A powerful instruction-tuned model, based on Llama 2, known for strong performance." },
            { name: "orca-mini", description: "A smaller, fine-tuned version of Orca, designed for efficient performance on various tasks." },
            { name: "medllama2", description: "A medical domain-specific version of Llama 2, useful for healthcare-related text generation." },
            { name: "nous-hermes2", description: "A strong conversational model, part of the Nous Research efforts." }
        ];

        async function fetchAndPopulateModels() {
            try {
                const response = await fetch('/api/models');
                if (!response.ok) {
                    const errorText = await response.text();
                    throw new Error("HTTP error! status: " + response.status + ", message: " + errorText);
                }
                const data = await response.json();
                
                modelSelect.innerHTML = ''; 
                modelActionSelect.innerHTML = '';

                if (data.models && data.models.length > 0) {
                    data.models.forEach(model => {
                        const option = document.createElement('option');
                        option.value = model.name;
                        option.textContent = model.name;
                        modelSelect.appendChild(option);

                        const actionOption = document.createElement('option');
                        actionOption.value = model.name;
                        actionOption.textContent = model.name;
                        modelActionSelect.appendChild(actionOption);
                    });
                    if (Array.from(modelSelect.options).some(option => option.value === 'llama2')) {
                        modelSelect.value = 'llama2';
                    } else {
                        modelSelect.selectedIndex = 0;
                    }
                    if (Array.from(modelActionSelect.options).some(option => option.value === 'llama2')) {
                        modelActionSelect.value = 'llama2';
                    } else {
                        modelActionSelect.selectedIndex = 0;
                    }
                    modelSelect.disabled = false;
                    modelActionSelect.disabled = false;
                    generateButton.disabled = false;
                    sendChatButton.disabled = false;
                    pullManualModelButton.disabled = false;
                    deleteModelButton.disabled = false;
                } else {
                    const option = document.createElement('option');
                    option.value = "";
                    option.textContent = "No models found. Run 'ollama pull <model_name>'";
                    modelSelect.appendChild(option);
                    modelActionSelect.appendChild(option.cloneNode(true));
                    
                    modelSelect.disabled = true;
                    modelActionSelect.disabled = true;
                    generateButton.disabled = true;
                    sendChatButton.disabled = true;
                    pullManualModelButton.disabled = true;
                    deleteModelButton.disabled = true;
                    showAlert("No Ollama models found. Please ensure Ollama is running and you have downloaded models (e.g., 'ollama pull llama2').");
                }

            } catch (error) {
                console.error('Error fetching models:', error);
                modelSelect.innerHTML = '<option value="">Error loading models</option>';
                modelActionSelect.innerHTML = '<option value="">Error loading models</option>';
                modelSelect.disabled = true;
                modelActionSelect.disabled = true;
                generateButton.disabled = true;
                sendChatButton.disabled = true;
                pullManualModelButton.disabled = true;
                deleteModelButton.disabled = true;
                let userMessage = 'Failed to load Ollama models. Please ensure Ollama is running on http://localhost:11434. Error: ' + error.message;
                showAlert(userMessage);
            }
        }

        function populateAvailableModels() {
            availableModelSelect.innerHTML = '';
            if (availableModels.length > 0) {
                availableModels.forEach(model => {
                    const option = document.createElement('option');
                    option.value = model.name;
                    option.textContent = model.name;
                    availableModelSelect.appendChild(option);
                });
                availableModelSelect.disabled = false;
                pullAvailableModelButton.disabled = false;
                availableModelSelect.dispatchEvent(new Event('change')); 
            } else {
                const option = document.createElement('option');
                option.value = "";
                option.textContent = "No available models listed.";
                availableModelSelect.appendChild(option);
                availableModelSelect.disabled = true;
                pullAvailableModelButton.disabled = true;
                availableModelDescription.classList.add('hidden');
            }
        }

        availableModelSelect.addEventListener('change', () => {
            const selectedModelName = availableModelSelect.value;
            const selectedModel = availableModels.find(model => model.name === selectedModelName);
            if (selectedModel && selectedModel.description) {
                availableModelDescription.textContent = selectedModel.description;
                availableModelDescription.classList.remove('hidden');
            } else {
                availableModelDescription.textContent = '';
                availableModelDescription.classList.add('hidden');
            }
        });

        function showSection(sectionId) {
            const sections = [generateSection, chatSection, modelManagementSection];
            sections.forEach(section => {
                if (section.id === sectionId) {
                    section.classList.remove('hidden');
                } else {
                    section.classList.add('hidden');
                }
            });

            if (sectionId === 'model-management-section') {
                commonModelSelectContainer.classList.add('hidden');
                advancedSettingsContainer.classList.add('hidden');
                unifiedResponseOutput.classList.add('hidden');
                populateAvailableModels();
            } else {
                commonModelSelectContainer.classList.remove('hidden');
                advancedSettingsContainer.classList.remove('hidden');
                unifiedResponseOutput.classList.remove('hidden');
            }
        }

        apiTypeSelect.addEventListener('change', (event) => {
            const selectedType = event.target.value;
            showSection(selectedType + '-section');
            responseOutput.textContent = '';
            modelActionOutput.textContent = '';
            thinkingOutput.textContent = '';
            thinkingOutput.classList.add('hidden');
            showThinkingCheckbox.checked = false;
            if (selectedType === 'chat') {
                chatMessages = [];
                chatHistoryOutput.innerHTML = '';
            }
        });

        document.addEventListener('DOMContentLoaded', () => {
            fetchAndPopulateModels();
            showSection(apiTypeSelect.value + '-section');
        });

        refreshModelsButton.addEventListener('click', fetchAndPopulateModels);

        // Stop generation button
        stopGenerateButton.addEventListener('click', () => {
            if (currentReader) {
                // Use catch to handle potential errors if stream is already closed
                currentReader.cancel().catch(e => console.log("Stream already cancelled or error during cancel:", e));
                currentReader = null;
                loadingIndicator.style.display = 'none';
                generateButton.disabled = false;
                stopGenerateButton.classList.add('hidden');
                modelSelect.disabled = false;
                apiTypeSelect.disabled = false;
                responseOutput.textContent += '\n\n[Generation stopped by user]';
            }
        });

        stopChatButton.addEventListener('click', () => {
            if (currentReader) {
                currentReader.cancel().catch(e => console.log("Stream already cancelled or error during cancel:", e));
                currentReader = null;
                loadingIndicator.style.display = 'none';
                sendChatButton.disabled = false;
                stopChatButton.classList.add('hidden');
                modelSelect.disabled = false;
                apiTypeSelect.disabled = false;
                thinkingOutput.textContent = '';
                thinkingOutput.classList.add('hidden');
            }
        });

        generateButton.addEventListener('click', async () => {
            const prompt = promptInput.value.trim();
            const model = modelSelect.value;
            if (!prompt) { showAlert('Please enter a prompt.'); return; }
            if (!model) { showAlert('Please select an Ollama model.'); return; }

            responseOutput.textContent = '';
            responseOutput.innerHTML = ''; // Clear HTML too
            loadingIndicator.style.display = 'block';
            generateButton.disabled = true;
            stopGenerateButton.classList.remove('hidden');
            modelSelect.disabled = true;
            apiTypeSelect.disabled = true;

            try {
                const response = await fetch('/api/ollama-action', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ 
                        actionType: 'generate', 
                        prompt, 
                        model,
                        options: getOptions()
                    }),
                });

                if (!response.ok) {
                    const errorText = await response.text();
                    throw new Error("HTTP error! status: " + response.status + ", message: " + errorText);
                }

                const reader = response.body.getReader();
                currentReader = reader;
                const decoder = new TextDecoder('utf-8');
                let buffer = '';
                let fullResponse = '';

                while (true) {
                    const { done, value } = await reader.read();
                    if (done) { break; }
                    buffer += decoder.decode(value, { stream: true });
                    const lines = buffer.split('\n');
                    buffer = lines.pop();

                    for (const line of lines) {
                        if (line.startsWith('data: ')) {
                            const data = line.substring(6);
                            if (data === '[DONE]') { reader.cancel(); break; }
                            try {
                                const jsonChunk = JSON.parse(data);
                                if (jsonChunk.response) {
                                    fullResponse += jsonChunk.response;
                                    // Display the content in chunks without full markdown re-render
                                    responseOutput.textContent = fullResponse;
                                    responseOutput.scrollTop = responseOutput.scrollHeight;
                                }
                            } catch (e) { console.warn('Could not parse JSON chunk:', data, e); }
                        }
                    }
                }
                
                // Final Markdown Render
                responseOutput.innerHTML = marked.parse(fullResponse);

            } catch (error) {
                console.error('Error:', error);
                let userMessage = 'An unexpected error occurred: ' + error.message;
                if (error.message.includes("Could not connect to Ollama")) {
                    userMessage = "Could not connect to Ollama. Please ensure Ollama is running on http://localhost:11434 and the model '" + model + "' is available (e.g., 'ollama run " + model + "').";
                } else if (error.message.includes("404")) {
                    userMessage = "Ollama API error: Model '" + model + "' not found. Please ensure the model is installed (e.g., 'ollama run " + model + "').";
                } else if (error.message.includes("400")) {
                    userMessage = "Ollama API error: Bad request. Check your prompt or model name.";
                } else if (error.message.includes("500")) {
                    userMessage = "Internal server error. Please check the Go application logs for details.";
                }
                showAlert(userMessage);
                responseOutput.textContent = userMessage;
            } finally {
                currentReader = null;
                loadingIndicator.style.display = 'none';
                generateButton.disabled = false;
                stopGenerateButton.classList.add('hidden');
                modelSelect.disabled = false;
                apiTypeSelect.disabled = false;
            }
        });

        // Enter to send in chat
        chatInput.addEventListener('keydown', (e) => {
            if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
                e.preventDefault(); // Prevent newline in textarea
                sendChatButton.click();
            }
        });

        sendChatButton.addEventListener('click', async () => {
            const userMessageContent = chatInput.value.trim();
            const model = modelSelect.value;
            if (!userMessageContent) { showAlert('Please enter a message.'); return; }
            if (!model) { showAlert('Please select an Ollama model.'); return; }

            // Add system prompt if this is the first message
            if (chatMessages.length === 0 && systemPromptInput.value.trim()) {
                chatMessages.push({ role: "system", content: systemPromptInput.value.trim() });
            }

            chatMessages.push({ role: "user", content: userMessageContent });
            appendChatMessage("user", userMessageContent);
            chatInput.value = '';

            thinkingOutput.textContent = '';
            if (showThinkingCheckbox.checked) {
                thinkingOutput.classList.remove('hidden');
            } else {
                thinkingOutput.classList.add('hidden');
            }

            loadingIndicator.style.display = 'block';
            sendChatButton.disabled = true;
            stopChatButton.classList.remove('hidden');
            modelSelect.disabled = true;
            apiTypeSelect.disabled = true;

            try {
                const response = await fetch('/api/ollama-action', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ 
                        actionType: 'chat', 
                        messages: chatMessages, 
                        model,
                        options: getOptions()
                    }),
                });

                if (!response.ok) {
                    const errorText = await response.text();
                    throw new Error("HTTP error! status: " + response.status + ", message: " + errorText);
                }

                const reader = response.body.getReader();
                currentReader = reader;
                const decoder = new TextDecoder('utf-8');
                let buffer = '';
                let assistantResponseContent = '';

                const assistantMessageDiv = document.createElement('div');
                assistantMessageDiv.classList.add('chat-message', 'assistant');
                
                // Add a dedicated content container
                const contentContainer = document.createElement('div');
                contentContainer.classList.add('message-content');
                assistantMessageDiv.appendChild(contentContainer);

                // Add copy button
                const copyBtn = document.createElement('button');
                copyBtn.classList.add('copy-button', 'bg-gray-600', 'hover:bg-gray-700', 'text-white', 'rounded');
                copyBtn.textContent = '📋';
                copyBtn.onclick = () => {
                    navigator.clipboard.writeText(assistantResponseContent).then(() => {
                        copyBtn.textContent = '✅';
                        setTimeout(() => copyBtn.textContent = '📋', 2000);
                    });
                };
                assistantMessageDiv.appendChild(copyBtn);
                
                chatHistoryOutput.appendChild(assistantMessageDiv);

                while (true) {
                    const { done, value } = await reader.read();
                    if (done) { break; }
                    buffer += decoder.decode(value, { stream: true });
                    const lines = buffer.split('\n');
                    buffer = lines.pop();

                    for (const line of lines) {
                        if (line.startsWith('data: ')) {
                            const data = line.substring(6);
                            if (data === '[DONE]') { reader.cancel(); break; }
                            try {
                                const jsonChunk = JSON.parse(data);
                                if (jsonChunk.message && jsonChunk.message.content) {
                                    assistantResponseContent += jsonChunk.message.content;
                                    
                                    // Display the streamed content (plain text for streaming)
                                    contentContainer.textContent = assistantResponseContent;

                                    if (showThinkingCheckbox.checked) {
                                        thinkingOutput.textContent += jsonChunk.message.content;
                                        thinkingOutput.scrollTop = thinkingOutput.scrollHeight;
                                    }
                                }
                            } catch (e) { console.warn('Could not parse JSON chunk:', data, e); }
                        }
                    }
                    chatHistoryOutput.scrollTop = chatHistoryOutput.scrollHeight;
                }
                
                // Final Markdown Render for assistant message
                contentContainer.innerHTML = marked.parse(assistantResponseContent);
                
                chatHistoryOutput.scrollTop = chatHistoryOutput.scrollHeight;

                if (assistantResponseContent) {
                    chatMessages.push({ role: "assistant", content: assistantResponseContent });
                }

            } catch (error) {
                console.error('Error:', error);
                let userMessage = 'An unexpected error occurred during chat: ' + error.message;
                if (error.message.includes("Could not connect to Ollama")) {
                    userMessage = "Could not connect to Ollama. Please ensure Ollama is running on http://localhost:11434 and the model '" + model + "' is available (e.g., 'ollama run " + model + "').";
                } else if (error.message.includes("404")) {
                    userMessage = "Ollama API error: Model '" + model + "' not found. Please ensure the model is installed (e.g., 'ollama run " + model + "').";
                } else if (error.message.includes("400")) {
                    userMessage = "Ollama API error: Bad request. Check your message or model name.";
                } else if (error.message.includes("500")) {
                    userMessage = "Internal server error. Please check the Go application logs for details.";
                }
                showAlert(userMessage);
                appendChatMessage("error", userMessage);
            } finally {
                currentReader = null;
                loadingIndicator.style.display = 'none';
                sendChatButton.disabled = false;
                stopChatButton.classList.add('hidden');
                modelSelect.disabled = false;
                apiTypeSelect.disabled = false;
                thinkingOutput.textContent = '';
                thinkingOutput.classList.add('hidden');
            }
        });

        showThinkingCheckbox.addEventListener('change', () => {
            if (showThinkingCheckbox.checked) {
                if (thinkingOutput.textContent !== '') {
                    thinkingOutput.classList.remove('hidden');
                }
            } else {
                thinkingOutput.classList.add('hidden');
            }
        });

        function appendChatMessage(role, content) {
            const messageDiv = document.createElement('div');
            messageDiv.classList.add('chat-message', role);
            
            // Add content container for markdown
            const contentContainer = document.createElement('div');
            contentContainer.classList.add('message-content');
            messageDiv.appendChild(contentContainer);
            
            if (role === 'user' || role === 'error') {
                contentContainer.textContent = content;
            } else {
                contentContainer.innerHTML = marked.parse(content);
            }
            
            // Add copy button
            if (role !== 'error') {
                const copyBtn = document.createElement('button');
                copyBtn.classList.add('copy-button', 'bg-gray-600', 'hover:bg-gray-700', 'text-white', 'rounded');
                copyBtn.textContent = '📋';
                copyBtn.onclick = () => {
                    navigator.clipboard.writeText(content).then(() => {
                        copyBtn.textContent = '✅';
                        setTimeout(() => copyBtn.textContent = '📋', 2000);
                    });
                };
                messageDiv.appendChild(copyBtn);
            }
            
            chatHistoryOutput.appendChild(messageDiv);
            chatHistoryOutput.scrollTop = chatHistoryOutput.scrollHeight;
        }

        async function performPullModel(modelName) {
            modelActionOutput.textContent = 'Pulling model ' + modelName + '... This may take a while.';
            loadingIndicator.style.display = 'block';
            pullManualModelButton.disabled = true;
            pullAvailableModelButton.disabled = true;
            deleteModelButton.disabled = true;
            modelSelect.disabled = true;
            apiTypeSelect.disabled = true;
            refreshModelsButton.disabled = true;
            availableModelSelect.disabled = true;

            try {
                const response = await fetch('/api/ollama-action', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ actionType: 'pull', model: modelName }),
                });

                const result = await response.text();
                if (!response.ok) {
                    throw new Error("HTTP error! status: " + response.status + ", message: " + result);
                }
                modelActionOutput.textContent = 'Pull successful for ' + modelName + ':\n' + result;
                await fetchAndPopulateModels();
            } catch (error) {
                console.error('Error pulling model:', error);
                let userMessage = 'Failed to pull model ' + modelName + '. Error: ' + error.message;
                showAlert(userMessage);
                modelActionOutput.textContent = userMessage;
            } finally {
                loadingIndicator.style.display = 'none';
                pullManualModelButton.disabled = false;
                pullAvailableModelButton.disabled = false;
                deleteModelButton.disabled = false;
                modelSelect.disabled = false;
                apiTypeSelect.disabled = false;
                refreshModelsButton.disabled = false;
                availableModelSelect.disabled = false;
            }
        }

        pullAvailableModelButton.addEventListener('click', async () => {
            const model = availableModelSelect.value;
            if (!model) {
                showAlert('Please select a model from the list to pull.');
                return;
            }
            performPullModel(model);
        });

        pullManualModelButton.addEventListener('click', async () => {
            const model = modelActionInput.value.trim();
            if (!model) {
                showAlert('Please enter a model name in the manual input field.');
                return;
            }
            performPullModel(model);
        });

        deleteModelButton.addEventListener('click', async () => {
            let model = modelActionInput.value.trim();
            if (!model) {
                model = modelActionSelect.value;
            }
            if (!model) { showAlert('Please enter or select a model name to delete.'); return; }

            const confirmed = await showConfirm('Are you sure you want to delete model: ' + model + '? This action cannot be undone.');
            if (!confirmed) {
                return;
            }

            modelActionOutput.textContent = 'Deleting model ' + model + '...';
            loadingIndicator.style.display = 'block';
            pullManualModelButton.disabled = true;
            pullAvailableModelButton.disabled = true;
            deleteModelButton.disabled = true;
            modelSelect.disabled = true;
            apiTypeSelect.disabled = true;
            refreshModelsButton.disabled = true;
            availableModelSelect.disabled = true;

            try {
                const response = await fetch('/api/ollama-action', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ actionType: 'delete', model }),
                });

                const result = await response.text();
                if (!response.ok) {
                    throw new Error("HTTP error! status: " + response.status + ", message: " + result);
                }
                modelActionOutput.textContent = 'Delete successful for ' + model + ':\n' + result;
                await fetchAndPopulateModels();
            } catch (error) {
                console.error('Error deleting model:', error);
                let userMessage = 'Failed to delete model ' + model + '. Error: ' + error.message;
                showAlert(userMessage);
                modelActionOutput.textContent = userMessage;
            } finally {
                loadingIndicator.style.display = 'none';
                pullManualModelButton.disabled = false;
                pullAvailableModelButton.disabled = false;
                deleteModelButton.disabled = false;
                modelSelect.disabled = false;
                apiTypeSelect.disabled = false;
                refreshModelsButton.disabled = false;
                availableModelSelect.disabled = false;
            }
        });

    </script>
</body>
</html>
`)
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

// callGenerateAPI handles the /api/generate endpoint
func callGenerateAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	ollamaReq := OllamaGenerateRequestPayload{
		Model:   clientReq.Model,
		Prompt:  clientReq.Prompt,
		Stream:  true,
		Options: clientReq.Options,
	}
	payloadBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "Error marshalling Ollama generate request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(http.MethodPost, ollamaGenerateAPI, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Error creating generate request to Ollama: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error connecting to Ollama generate API: %v", err)
		http.Error(w, "Could not connect to Ollama. Please ensure Ollama is running on "+ollamaBaseURL+". "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Ollama generate API returned non-200 status: %d, body: %s", resp.StatusCode, string(bodyBytes))
		http.Error(w, fmt.Sprintf("Ollama API error: Status %d, Message: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes))), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("Streaming not supported by this connection for generate API.")
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
			log.Printf("Error unmarshalling Ollama generate response chunk: %v, line: %s", err, line)
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

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading Ollama generate response stream: %v", err)
	}
}

// callChatAPI handles the /api/chat endpoint
func callChatAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	ollamaReq := OllamaChatRequestPayload{
		Model:    clientReq.Model,
		Messages: clientReq.Messages,
		Stream:   true,
		Options:  clientReq.Options,
	}
	payloadBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "Error marshalling Ollama chat request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(http.MethodPost, ollamaChatAPI, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Error creating chat request to Ollama: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error connecting to Ollama chat API: %v", err)
		http.Error(w, "Could not connect to Ollama. Please ensure Ollama is running on "+ollamaBaseURL+". "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Ollama chat API returned non-200 status: %d, body: %s", resp.StatusCode, string(bodyBytes))
		http.Error(w, fmt.Sprintf("Ollama API error: Status %d, Message: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes))), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Println("Streaming not supported by this connection for chat API.")
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
			log.Printf("Error unmarshalling Ollama chat response chunk: %v, line: %s", err, line)
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
		log.Printf("Error reading Ollama chat response stream: %v", err)
	}
}

// callModelPullAPI handles the /api/pull endpoint
func callModelPullAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	ollamaReq := OllamaModelActionPayload{
		Name: clientReq.Model,
	}
	payloadBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "Error marshalling Ollama pull request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(http.MethodPost, ollamaPullAPI, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Error creating pull request to Ollama: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error connecting to Ollama pull API: %v", err)
		http.Error(w, "Could not connect to Ollama. Please ensure Ollama is running on "+ollamaBaseURL+". "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Error reading Ollama pull response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Ollama pull API returned non-200 status: %d, body: %s", resp.StatusCode, string(bodyBytes))
		http.Error(w, fmt.Sprintf("Ollama API error pulling model: Status %d, Message: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes))), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write(bodyBytes)
}

// callModelDeleteAPI handles the /api/delete endpoint
func callModelDeleteAPI(w http.ResponseWriter, r *http.Request, clientReq ClientRequest, client *http.Client) {
	ollamaReq := OllamaModelActionPayload{
		Name: clientReq.Model,
	}
	payloadBytes, err := json.Marshal(ollamaReq)
	if err != nil {
		http.Error(w, "Error marshalling Ollama delete request: "+err.Error(), http.StatusInternalServerError)
		return
	}

	req, err := http.NewRequest(http.MethodDelete, ollamaDeleteAPI, bytes.NewBuffer(payloadBytes))
	if err != nil {
		http.Error(w, "Error creating delete request to Ollama: "+err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Error connecting to Ollama delete API: %v", err)
		http.Error(w, "Could not connect to Ollama. Please ensure Ollama is running on "+ollamaBaseURL+". "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, "Error reading Ollama delete response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("Ollama delete API returned non-200 status: %d, body: %s", resp.StatusCode, string(bodyBytes))
		http.Error(w, fmt.Sprintf("Ollama API error deleting model: Status %d, Message: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes))), resp.StatusCode)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Write(bodyBytes)
}

// handleListModels fetches the list of available Ollama models from the /api/tags endpoint.
func handleListModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(ollamaTagsAPI)
	if err != nil {
		log.Printf("Error connecting to Ollama tags API: %v", err)
		http.Error(w, "Could not connect to Ollama to list models. Please ensure Ollama is running on "+ollamaTagsAPI+".", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("Ollama tags API returned non-200 status: %d, body: %s", resp.StatusCode, string(bodyBytes))
		http.Error(w, fmt.Sprintf("Ollama API error fetching models: Status %d, Message: %s", resp.StatusCode, strings.TrimSpace(string(bodyBytes))), resp.StatusCode)
		return
	}

	var tagsResponse OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
		log.Printf("Error unmarshalling Ollama tags response: %v", err)
		http.Error(w, "Error parsing Ollama models response.", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tagsResponse)
}