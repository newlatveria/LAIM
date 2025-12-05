package main

import (
	"bufio" // <-- FIXED: Added missing import for streaming
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type Config struct {
	Port           string
	OllamaURL      string
	DatabasePath   string
	UploadDir      string
	MaxUploadSize  int64
	AllowedOrigins []string
}

type Server struct {
	db          *sql.DB
	config      *Config
	sessions    map[string]*Session
	sessionsMux sync.RWMutex
	rateLimiter *RateLimiter
}

type Session struct {
	ID        string
	UserID    string
	CreatedAt time.Time
	LastSeen  time.Time
}

type Chat struct {
	ID        string    `json:"id"`
	SessionID string    `json:"session_id"`
	Title     string    `json:"title"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Message struct {
	ID      string `json:"id"`
	ChatID  string `json:"chat_id"`
	Role    string `json:"role"`
	Content string `json:"content"`
	Files     []File    `json:"files,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type File struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Content  string `json:"content"` // Base64 encoded content
}

type RateLimiter struct {
	limit   int
	data    map[string]int64 // <-- FIXED: Changed int62 to int64
	dataMux sync.Mutex
}

func NewRateLimiter(limit int) *RateLimiter {
	return &RateLimiter{
		limit: limit,
		data:  make(map[string]int64), // <-- FIXED: Changed int62 to int64
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.dataMux.Lock()
	defer rl.dataMux.Unlock()

	// Simple rate limit check
	if rl.data[key] < int64(rl.limit) {
		rl.data[key]++
		return true
	}
	return false
}

func main() {
	// 1. Load Configuration
	config := &Config{
		Port:           os.Getenv("PORT"),
		OllamaURL:      os.Getenv("OLLAMA_URL"),
		DatabasePath:   os.Getenv("DATABASE_PATH"),
		UploadDir:      os.Getenv("UPLOAD_DIR"),
		MaxUploadSize:  10 << 20, // 10MB limit
		AllowedOrigins: strings.Split(os.Getenv("ALLOWED_ORIGINS"), ","),
	}

	if config.Port == "" {
		config.Port = "8080"
	}
	if config.OllamaURL == "" {
		config.OllamaURL = "http://localhost:11434"
	}
	if config.DatabasePath == "" {
		config.DatabasePath = "./laim.db"
	}
	if config.UploadDir == "" {
		config.UploadDir = "./uploads"
	}

	// 2. Database Initialization
	db, err := initDB(config.DatabasePath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// 3. Server Setup
	s := &Server{
		db:          db,
		config:      config,
		sessions:    make(map[string]*Session),
		rateLimiter: NewRateLimiter(100),
	}

	// 4. Register Handlers
	http.HandleFunc("/", s.serveHTML)
	http.Handle("/style.css", http.FileServer(http.Dir(".")))
	http.Handle("/script.js", http.FileServer(http.Dir(".")))
	http.Handle("/favicon.ico", http.FileServer(http.Dir("."))) // Fix for 404

	// API Handlers
	http.HandleFunc("/api/session", s.handleNewSession)
	http.HandleFunc("/api/chats", s.handleListChats)
	http.HandleFunc("/api/chat", s.handleNewChat)
	http.HandleFunc("/api/chat/title", s.handleUpdateChatTitle)
	http.HandleFunc("/api/messages", s.handleNewMessage)
	http.HandleFunc("/api/messages/", s.handleListMessages)
	http.HandleFunc("/api/models", s.handleListModels)
	http.HandleFunc("/api/models/pull", s.handlePullModel)
	http.HandleFunc("/api/models/delete", s.handleDeleteModel)

	// 5. Start Server
	log.Printf("Starting server on :%s", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, nil))
}

func initDB(dbPath string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}

	// Create tables if they don't exist
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS chats (
		id TEXT PRIMARY KEY,
		session_id TEXT,
		title TEXT,
		model TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (session_id) REFERENCES sessions(id)
	);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		chat_id TEXT,
		role TEXT,
		content TEXT,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (chat_id) REFERENCES chats(id)
	);

	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		message_id TEXT,
		name TEXT,
		mime_type TEXT,
		content_base64 TEXT,
		FOREIGN KEY (message_id) REFERENCES messages(id)
	);
	`
	_, err = db.Exec(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return db, nil
}

// --- Middleware and Helpers ---

func (s *Server) sendJSON(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Error sending JSON response: %v", err)
	}
}

func (s *Server) sendError(w http.ResponseWriter, message string, code string, statusCode int) {
	log.Printf("Error %d (%s): %s", statusCode, code, message)
	s.sendJSON(w, map[string]string{
		"error": message,
		"code":  code,
	}, statusCode)
}

func (s *Server) getSessionID(r *http.Request) (string, error) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		return "", fmt.Errorf("X-Session-ID header missing")
	}
	return sessionID, nil
}

// --- Session Handlers ---

func (s *Server) handleNewSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	sessionID := uuid.New().String()
	
	newSession := &Session{
		ID:        sessionID,
		UserID:    "anonymous",
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
	}

	_, err := s.db.Exec("INSERT INTO sessions (id, user_id) VALUES (?, ?)", newSession.ID, newSession.UserID)
	if err != nil {
		s.sendError(w, "Could not create session in DB", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	s.sessionsMux.Lock()
	s.sessions[sessionID] = newSession
	s.sessionsMux.Unlock()

	s.sendJSON(w, map[string]string{"session_id": sessionID}, http.StatusOK)
}

// --- Chat Handlers ---

func (s *Server) handleListChats(w http.ResponseWriter, r *http.Request) {
	sessionID, err := s.getSessionID(r)
	if err != nil {
		s.sendError(w, "Unauthorized: "+err.Error(), "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}

	rows, err := s.db.Query("SELECT id, session_id, title, model, created_at, updated_at FROM chats WHERE session_id = ? ORDER BY updated_at DESC", sessionID)
	if err != nil {
		s.sendError(w, "Database query failed", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var chats []Chat
	for rows.Next() {
		var chat Chat
		if err := rows.Scan(&chat.ID, &chat.SessionID, &chat.Title, &chat.Model, &chat.CreatedAt, &chat.UpdatedAt); err != nil {
			log.Printf("Error scanning chat row: %v", err)
			continue
		}
		chats = append(chats, chat)
	}

	s.sendJSON(w, chats, http.StatusOK)
}

func (s *Server) handleNewChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	sessionID, err := s.getSessionID(r)
	if err != nil {
		// Return 401 for missing session ID (Fix for 500 error from previous round)
		s.sendError(w, "Session ID required to create chat", "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}

	// Parse request body for initial model (optional)
	var req struct {
		Model string `json:"model"`
	}
	// It's okay if this decode fails, we will use a default model
	json.NewDecoder(r.Body).Decode(&req) 
	
	newChat := Chat{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Title:     "New Chat",
		Model:     req.Model, 
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	_, err = s.db.Exec("INSERT INTO chats (id, session_id, title, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		newChat.ID, newChat.SessionID, newChat.Title, newChat.Model, newChat.CreatedAt, newChat.UpdatedAt)
	if err != nil {
		s.sendError(w, "Could not save chat to DB", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	s.sendJSON(w, newChat, http.StatusCreated)
}

func (s *Server) handleUpdateChatTitle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	_, err := s.getSessionID(r)
	if err != nil {
		s.sendError(w, "Unauthorized", "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}

	var req struct {
		ChatID string `json:"chat_id"`
		Title  string `json:"title"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	if req.ChatID == "" || req.Title == "" {
		s.sendError(w, "ChatID and Title are required", "INVALID_INPUT", http.StatusBadRequest)
		return
	}

	_, err = s.db.Exec("UPDATE chats SET title = ?, updated_at = ? WHERE id = ?", req.Title, time.Now(), req.ChatID)
	if err != nil {
		s.sendError(w, "Failed to update chat title", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	s.sendJSON(w, map[string]string{"status": "success"}, http.StatusOK)
}

// --- Message Handlers ---

func (s *Server) handleListMessages(w http.ResponseWriter, r *http.Request) {
	sessionID, err := s.getSessionID(r)
	if err != nil {
		s.sendError(w, "Unauthorized: "+err.Error(), "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}
	
	// Extract chat ID from URL path (e.g., /api/messages/{chat_id})
	pathSegments := strings.Split(r.URL.Path, "/")
	if len(pathSegments) < 4 || pathSegments[3] == "" {
		s.sendError(w, "Chat ID missing from URL", "INVALID_URL", http.StatusBadRequest)
		return
	}
	chatID := pathSegments[3]

	// Check to ensure the chat belongs to the session (prevents cross-session viewing)
	var chatSessionID string
	err = s.db.QueryRow("SELECT session_id FROM chats WHERE id = ?", chatID).Scan(&chatSessionID)
	if err == sql.ErrNoRows {
		s.sendError(w, "Chat not found", "NOT_FOUND", http.StatusNotFound)
		return
	}
	if err != nil {
		s.sendError(w, "Database error checking chat ownership", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	if chatSessionID != sessionID {
		s.sendError(w, "Access forbidden to this chat", "FORBIDDEN", http.StatusForbidden)
		return
	}

	rows, err := s.db.Query("SELECT id, chat_id, role, content, created_at FROM messages WHERE chat_id = ? ORDER BY created_at ASC", chatID)
	if err != nil {
		s.sendError(w, "Database query failed", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		if err := rows.Scan(&msg.ID, &msg.ChatID, &msg.Role, &msg.Content, &msg.CreatedAt); err != nil {
			log.Printf("Error scanning message row: %v", err)
			continue
		}
		messages = append(messages, msg)
	}

	s.sendJSON(w, messages, http.StatusOK)
}

func (s *Server) handleNewMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	sessionID, err := s.getSessionID(r)
	if err != nil {
		s.sendError(w, "Unauthorized", "UNAUTHORIZED", http.StatusUnauthorized)
		return
	}

	var userMessage struct {
		ChatID  string `json:"chat_id"`
		Content string `json:"content"`
		Model   string `json:"model"`
		Files   []File `json:"files,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&userMessage); err != nil {
		s.sendError(w, "Invalid JSON request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Validation (Fix for 400 Bad Request error)
	if userMessage.ChatID == "" || userMessage.Content == "" || userMessage.Model == "" {
		s.sendError(w, "ChatID, Content, and Model are required fields", "MISSING_FIELDS", http.StatusBadRequest)
		return
	}

	// Security Check (Fix for 'sessionID declared but not used' error)
	var chatSessionID string
	err = s.db.QueryRow("SELECT session_id FROM chats WHERE id = ?", userMessage.ChatID).Scan(&chatSessionID)
	if err == sql.ErrNoRows {
		s.sendError(w, "Chat not found", "NOT_FOUND", http.StatusNotFound)
		return
	}
	if err != nil {
		s.sendError(w, "Database error checking chat ownership", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	if chatSessionID != sessionID {
		s.sendError(w, "Access forbidden to this chat", "FORBIDDEN", http.StatusForbidden)
		return
	}
	// sessionID is now used.

	// 1. Save User Message
	userMsg := Message{
		ID:      uuid.New().String(),
		ChatID:  userMessage.ChatID,
		Role:    "user",
		Content: userMessage.Content,
		CreatedAt: time.Now(),
	}

	_, err = s.db.Exec("INSERT INTO messages (id, chat_id, role, content) VALUES (?, ?, ?, ?)",
		userMsg.ID, userMsg.ChatID, userMsg.Role, userMsg.Content)
	if err != nil {
		s.sendError(w, "Could not save user message to DB", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	// 2. Prepare Ollama Request Payload
	// Update the chat's updated_at timestamp
	_, err = s.db.Exec("UPDATE chats SET updated_at = ?, model = ? WHERE id = ?", time.Now(), userMessage.Model, userMessage.ChatID)
	if err != nil {
		log.Printf("Warning: Could not update chat timestamp: %v", err)
	}

	ollamaMessages := []map[string]interface{}{
		{"role": "user", "content": userMessage.Content},
	}
	
	ollamaReq := map[string]interface{}{
		"model": userMessage.Model,
		"messages": ollamaMessages,
		"stream":  true,
	}
	
	if len(userMessage.Files) > 0 {
		var images []string
		for _, file := range userMessage.Files {
			images = append(images, file.Content)
		}
		ollamaMessages[0]["images"] = images
	}

	ollamaBody, _ := json.Marshal(ollamaReq)

	// 3. Make Ollama Request (Streaming)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	ollamaURL := s.config.OllamaURL + "/api/chat" 
	
	httpReq, err := http.NewRequestWithContext(ctx, "POST", ollamaURL, bytes.NewBuffer(ollamaBody))
	if err != nil {
		s.sendError(w, "Failed to create Ollama request", "REQUEST_ERROR", http.StatusInternalServerError)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	ollamaResp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Printf("Error calling Ollama: %v", err)
		s.sendError(w, "Failed to connect to Ollama", "OLLAMA_ERROR", http.StatusServiceUnavailable)
		return
	}
	defer ollamaResp.Body.Close()

	if ollamaResp.StatusCode != http.StatusOK {
		var errBody map[string]interface{}
		json.NewDecoder(ollamaResp.Body).Decode(&errBody)
		log.Printf("Ollama API returned error status %d: %v", ollamaResp.StatusCode, errBody)
		s.sendError(w, fmt.Sprintf("Ollama API Error: %v", errBody["error"]), "OLLAMA_API_ERROR", http.StatusBadGateway)
		return
	}

	// 4. Stream Ollama Response to Client and Collect Full Message
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	reader := bufio.NewReader(ollamaResp.Body) // <-- Use bufio here
	fullResponseContent := ""
	
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading stream: %v", err)
			}
			break
		}

		// Write raw data to client immediately (for streaming)
		w.Write(line)
		w.(http.Flusher).Flush()

		// Parse the line for collecting the full response
		var chunk map[string]interface{}
		if err := json.Unmarshal(line, &chunk); err == nil {
			if msg, ok := chunk["message"].(map[string]interface{}); ok {
				if content, ok := msg["content"].(string); ok {
					fullResponseContent += content
				}
			} else if finalContent, ok := chunk["content"].(string); ok {
				fullResponseContent += finalContent
			}
		}
		
		if chunk["done"] == true {
			break
		}
	}

	// 5. Save Model Response
	if fullResponseContent != "" {
		modelMsg := Message{
			ID:      uuid.New().String(),
			ChatID:  userMessage.ChatID,
			Role:    "assistant",
			Content: fullResponseContent,
			CreatedAt: time.Now(),
		}

		_, err = s.db.Exec("INSERT INTO messages (id, chat_id, role, content) VALUES (?, ?, ?, ?)",
			modelMsg.ID, modelMsg.ChatID, modelMsg.Role, modelMsg.Content)
		if err != nil {
			log.Printf("Warning: Could not save model message to DB: %v", err)
		}
	}
}

// --- Ollama Model Handlers ---

func (s *Server) handleListModels(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(s.config.OllamaURL + "/api/tags")
	if err != nil {
		s.sendError(w, "Failed to connect to Ollama", "OLLAMA_ERROR", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.sendError(w, "Ollama returned non-200 status", "OLLAMA_API_ERROR", http.StatusBadGateway)
		return
	}

	var ollamaResponse struct {
		Models []struct {
			Name string `json:"name"`
			Size int64 `json:"size"`
		} `json:"models"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&ollamaResponse); err != nil {
		s.sendError(w, "Failed to decode Ollama response", "DECODE_ERROR", http.StatusInternalServerError)
		return
	}

	// Respond with just the list of model names
	modelNames := []string{}
	for _, model := range ollamaResponse.Models {
		modelNames = append(modelNames, model.Name)
	}

	s.sendJSON(w, modelNames, http.StatusOK)
}

func (s *Server) handlePullModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	reqBody, _ := json.Marshal(req)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.config.OllamaURL+"/api/pull", bytes.NewBuffer(reqBody))
	if err != nil {
		s.sendError(w, "Failed to create request", "REQUEST_ERROR", http.StatusInternalServerError)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Printf("Error calling Ollama: %v", err)
		s.sendError(w, "Failed to connect to Ollama", "OLLAMA_ERROR", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	// Stream the response from Ollama directly to the client
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(resp.StatusCode)

	if resp.StatusCode == http.StatusOK {
		// Use io.Copy to stream the body for pull progress
		io.Copy(w, resp.Body)
	} else {
		// Handle non-200 responses by sending the error back
		io.Copy(w, resp.Body)
	}
}

func (s *Server) handleDeleteModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	reqBody, _ := json.Marshal(req)
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "DELETE", s.config.OllamaURL+"/api/delete", bytes.NewBuffer(reqBody))
	if err != nil {
		s.sendError(w, "Failed to create request", "REQUEST_ERROR", http.StatusInternalServerError)
		return
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Printf("Error calling Ollama: %v", err)
		s.sendError(w, "Failed to connect to Ollama", "OLLAMA_ERROR", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) serveHTML(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	
	content, err := os.ReadFile("index.html")
	if err != nil {
		http.Error(w, "Could not read index.html", http.StatusInternalServerError)
		return
	}
	w.Write(content)
}