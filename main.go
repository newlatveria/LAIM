package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
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
	ID        string    `json:"id"`
	ChatID    string    `json:"chat_id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Files     []File    `json:"files,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type File struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type RateLimiter struct {
	requests map[string]*RequestCounter
	mu       sync.RWMutex
}

type RequestCounter struct {
	count     int
	resetTime time.Time
}

var allowedFileTypes = map[string]bool{
	"image/jpeg":      true,
	"image/png":       true,
	"image/gif":       true,
	"image/webp":      true,
	"text/plain":      true,
	"application/pdf": true,
}

func main() {
	config := LoadConfig()

	server, err := NewServer(config)
	if err != nil {
		log.Fatal(err)
	}
	defer server.db.Close()

	// Middleware setup
	mux := http.NewServeMux()

	// Static file routes
	mux.HandleFunc("/style.css", server.serveCSS)
	mux.HandleFunc("/script.js", server.serveJS)

	// Session endpoints
	mux.HandleFunc("/api/session", server.handleSession)

	// Chat endpoints
	mux.HandleFunc("/api/chats", server.withAuth(server.withRateLimit(server.handleChats)))
	mux.HandleFunc("/api/chats/", server.withAuth(server.withRateLimit(server.handleChatDetail)))
	mux.HandleFunc("/api/messages", server.withAuth(server.withRateLimit(server.handleMessages)))

	// File upload endpoints
	mux.HandleFunc("/api/upload", server.withAuth(server.withRateLimit(server.handleUpload)))
	mux.HandleFunc("/api/files/", server.handleFileServe)

	// Ollama endpoints
	mux.HandleFunc("/api/generate", server.withAuth(server.withRateLimit(server.handleGenerate)))
	mux.HandleFunc("/api/chat", server.withAuth(server.withRateLimit(server.handleOllamaChat)))
	mux.HandleFunc("/api/models", server.handleModels)
	mux.HandleFunc("/api/pull", server.withAuth(server.handlePull))
	mux.HandleFunc("/api/delete", server.withAuth(server.handleDelete))

	// Root handler
	mux.HandleFunc("/", server.serveHTML)

	// Apply CORS and logging middleware to all routes
	handler := corsMiddleware(loggingMiddleware(mux), config.AllowedOrigins)

	log.Printf("Server starting on http://localhost:%s\n", config.Port)
	log.Fatal(http.ListenAndServe(":"+config.Port, handler))
}

func LoadConfig() *Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}

	allowedOrigins := strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",")
	if len(allowedOrigins) == 0 || allowedOrigins[0] == "" {
		allowedOrigins = []string{"*"}
	}

	return &Config{
		Port:           port,
		OllamaURL:      ollamaURL,
		DatabasePath:   "./laim.db",
		UploadDir:      "./uploads",
		MaxUploadSize:  100 << 20, // 100MB
		AllowedOrigins: allowedOrigins,
	}
}

func NewServer(config *Config) (*Server, error) {
	db, err := sql.Open("sqlite3", config.DatabasePath)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	server := &Server{
		db:          db,
		config:      config,
		sessions:    make(map[string]*Session),
		rateLimiter: NewRateLimiter(),
	}

	if err := server.initDB(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(config.UploadDir, 0755); err != nil {
		return nil, err
	}

	return server, nil
}

func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		requests: make(map[string]*RequestCounter),
	}
	
	// Cleanup old entries every minute
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()
	
	return rl
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	counter, exists := rl.requests[key]

	if !exists || now.After(counter.resetTime) {
		rl.requests[key] = &RequestCounter{
			count:     1,
			resetTime: now.Add(time.Minute),
		}
		return true
	}

	if counter.count >= 60 { // 60 requests per minute
		return false
	}

	counter.count++
	return true
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for key, counter := range rl.requests {
		if now.After(counter.resetTime) {
			delete(rl.requests, key)
		}
	}
}

// Middleware functions
func corsMiddleware(next http.Handler, allowedOrigins []string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		
		// Check if origin is allowed
		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			if allowedOrigin == "*" || allowedOrigin == origin {
				allowed = true
				break
			}
		}

		if allowed {
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else if allowedOrigins[0] == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Session-ID")
			w.Header().Set("Access-Control-Max-Age", "3600")
		}

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Create a custom response writer to capture status code
		lrw := &loggingResponseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		
		next.ServeHTTP(lrw, r)
		
		log.Printf("[%s] %s %s - %d (%v)",
			r.Method,
			r.URL.Path,
			r.RemoteAddr,
			lrw.statusCode,
			time.Since(start),
		)
	})
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.Header.Get("X-Session-ID")
		if sessionID == "" {
			s.sendError(w, "Session ID required", "AUTH_REQUIRED", http.StatusUnauthorized)
			return
		}

		// Validate session exists
		s.sessionsMux.RLock()
		_, exists := s.sessions[sessionID]
		s.sessionsMux.RUnlock()

		if !exists {
			// Check database
			var count int
			err := s.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE id = ?", sessionID).Scan(&count)
			if err != nil || count == 0 {
				s.sendError(w, "Invalid session", "INVALID_SESSION", http.StatusUnauthorized)
				return
			}
		}

		next(w, r)
	}
}

func (s *Server) withRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := r.Header.Get("X-Session-ID")
		if sessionID == "" {
			sessionID = r.RemoteAddr
		}

		if !s.rateLimiter.Allow(sessionID) {
			s.sendError(w, "Rate limit exceeded", "RATE_LIMIT", http.StatusTooManyRequests)
			return
		}

		next(w, r)
	}
}

func (s *Server) sendError(w http.ResponseWriter, message, code string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(ErrorResponse{
		Error:   message,
		Code:    code,
		Message: message,
	})
}

func (s *Server) initDB() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT,
		created_at DATETIME,
		last_seen DATETIME
	);

	CREATE TABLE IF NOT EXISTS chats (
		id TEXT PRIMARY KEY,
		session_id TEXT,
		title TEXT,
		model TEXT,
		created_at DATETIME,
		updated_at DATETIME,
		FOREIGN KEY(session_id) REFERENCES sessions(id)
	);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		chat_id TEXT,
		role TEXT,
		content TEXT,
		created_at DATETIME,
		FOREIGN KEY(chat_id) REFERENCES chats(id)
	);

	CREATE TABLE IF NOT EXISTS files (
		id TEXT PRIMARY KEY,
		message_id TEXT,
		name TEXT,
		path TEXT,
		mime_type TEXT,
		size INTEGER,
		created_at DATETIME,
		FOREIGN KEY(message_id) REFERENCES messages(id)
	);

	CREATE INDEX IF NOT EXISTS idx_chats_session ON chats(session_id);
	CREATE INDEX IF NOT EXISTS idx_messages_chat ON messages(chat_id);
	CREATE INDEX IF NOT EXISTS idx_files_message ON files(message_id);
	CREATE INDEX IF NOT EXISTS idx_chats_updated ON chats(updated_at DESC);
	`

	_, err := s.db.Exec(schema)
	return err
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	sessionID := uuid.New().String()
	session := &Session{
		ID:        sessionID,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (id, user_id, created_at, last_seen) VALUES (?, ?, ?, ?)",
		session.ID, session.UserID, session.CreatedAt, session.LastSeen,
	)
	if err != nil {
		log.Printf("Error creating session: %v", err)
		s.sendError(w, "Failed to create session", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	s.sessionsMux.Lock()
	s.sessions[sessionID] = session
	s.sessionsMux.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"session_id": sessionID})
}

func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")

	switch r.Method {
	case "GET":
		s.getChats(w, r, sessionID)
	case "POST":
		s.createChat(w, r, sessionID)
	default:
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getChats(w http.ResponseWriter, r *http.Request, sessionID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, session_id, title, model, created_at, updated_at FROM chats WHERE session_id = ? ORDER BY updated_at DESC",
		sessionID,
	)
	if err != nil {
		log.Printf("Error querying chats: %v", err)
		s.sendError(w, "Failed to load chats", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var chats []Chat
	for rows.Next() {
		var chat Chat
		err := rows.Scan(&chat.ID, &chat.SessionID, &chat.Title, &chat.Model, &chat.CreatedAt, &chat.UpdatedAt)
		if err != nil {
			log.Printf("Error scanning chat: %v", err)
			continue
		}
		chats = append(chats, chat)
	}

	if chats == nil {
		chats = []Chat{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chats)
}

func (s *Server) createChat(w http.ResponseWriter, r *http.Request, sessionID string) {
	var req struct {
		Title string `json:"title"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Validate inputs
	if len(req.Title) > 200 {
		s.sendError(w, "Title too long (max 200 characters)", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if req.Title == "" {
		req.Title = "New Chat"
	}

	chat := Chat{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Title:     req.Title,
		Model:     req.Model,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO chats (id, session_id, title, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
		chat.ID, chat.SessionID, chat.Title, chat.Model, chat.CreatedAt, chat.UpdatedAt,
	)
	if err != nil {
		log.Printf("Error creating chat: %v", err)
		s.sendError(w, "Failed to create chat", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(chat)
}

func (s *Server) handleChatDetail(w http.ResponseWriter, r *http.Request) {
	chatID := strings.TrimPrefix(r.URL.Path, "/api/chats/")
	if chatID == "" {
		s.sendError(w, "Chat ID required", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		s.getChatMessages(w, r, chatID)
	case "DELETE":
		s.deleteChat(w, r, chatID)
	case "PUT":
		s.updateChat(w, r, chatID)
	default:
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getChatMessages(w http.ResponseWriter, r *http.Request, chatID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, chat_id, role, content, created_at FROM messages WHERE chat_id = ? ORDER BY created_at ASC",
		chatID,
	)
	if err != nil {
		log.Printf("Error querying messages: %v", err)
		s.sendError(w, "Failed to load messages", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		err := rows.Scan(&msg.ID, &msg.ChatID, &msg.Role, &msg.Content, &msg.CreatedAt)
		if err != nil {
			log.Printf("Error scanning message: %v", err)
			continue
		}

		// Get files for this message
		fileRows, err := s.db.QueryContext(ctx,
			"SELECT id, name, path, mime_type, size FROM files WHERE message_id = ?",
			msg.ID,
		)
		if err == nil {
			for fileRows.Next() {
				var file File
				if err := fileRows.Scan(&file.ID, &file.Name, &file.Path, &file.MimeType, &file.Size); err == nil {
					msg.Files = append(msg.Files, file)
				}
			}
			fileRows.Close()
		}

		messages = append(messages, msg)
	}

	if messages == nil {
		messages = []Message{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

func (s *Server) deleteChat(w http.ResponseWriter, r *http.Request, chatID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx, "DELETE FROM chats WHERE id = ?", chatID)
	if err != nil {
		log.Printf("Error deleting chat: %v", err)
		s.sendError(w, "Failed to delete chat", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) updateChat(w http.ResponseWriter, r *http.Request, chatID string) {
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	if len(req.Title) > 200 {
		s.sendError(w, "Title too long (max 200 characters)", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		"UPDATE chats SET title = ?, updated_at = ? WHERE id = ?",
		req.Title, time.Now(), chatID,
	)
	if err != nil {
		log.Printf("Error updating chat: %v", err)
		s.sendError(w, "Failed to update chat", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"message": "Chat updated"})
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ChatID  string   `json:"chat_id"`
		Role    string   `json:"role"`
		Content string   `json:"content"`
		FileIDs []string `json:"file_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	// Validate inputs
	if req.Role != "user" && req.Role != "assistant" && req.Role != "system" {
		s.sendError(w, "Invalid role", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}
	if len(req.Content) > 50000 {
		s.sendError(w, "Content too long (max 50000 characters)", "VALIDATION_ERROR", http.StatusBadRequest)
		return
	}

	msgID := uuid.New().String()
	now := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO messages (id, chat_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)",
		msgID, req.ChatID, req.Role, req.Content, now,
	)
	if err != nil {
		log.Printf("Error creating message: %v", err)
		s.sendError(w, "Failed to create message", "DB_ERROR", http.StatusInternalServerError)
		return
	}

	// Link files to message
	for _, fileID := range req.FileIDs {
		_, err := s.db.ExecContext(ctx, "UPDATE files SET message_id = ? WHERE id = ?", msgID, fileID)
		if err != nil {
			log.Printf("Error linking file %s to message: %v", fileID, err)
		}
	}

	// Update chat timestamp
	s.db.ExecContext(ctx, "UPDATE chats SET updated_at = ? WHERE id = ?", now, req.ChatID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": msgID})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.sendError(w, "Method not allowed", "METHOD_NOT_ALLOWED", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(s.config.MaxUploadSize); err != nil {
		s.sendError(w, "File too large", "FILE_TOO_LARGE", http.StatusBadRequest)
		return
	}

	var uploadedFiles []File
	files := r.MultipartForm.File["files"]

	for _, fileHeader := range files {
		// Validate file type
		contentType := fileHeader.Header.Get("Content-Type")
		if !allowedFileTypes[contentType] {
			log.Printf("Rejected file type: %s", contentType)
			continue
		}

		// Validate file size
		if fileHeader.Size > 50<<20 { // 50MB per file
			log.Printf("File too large: %d bytes", fileHeader.Size)
			continue
		}

		file, err := fileHeader.Open()
		if err != nil {
			log.Printf("Error opening file: %v", err)
			continue
		}
		defer file.Close()

		fileID := uuid.New().String()
		ext := filepath.Ext(fileHeader.Filename)
		
		// Sanitize extension
		if len(ext) > 10 {
			ext = ext[:10]
		}
		
		filename := fileID + ext
		filePath := filepath.Join(s.config.UploadDir, filename)

		dst, err := os.Create(filePath)
		if err != nil {
			log.Printf("Error creating file: %v", err)
			continue
		}
		defer dst.Close()

		size, err := io.Copy(dst, file)
		if err != nil {
			log.Printf("Error copying file: %v", err)
			os.Remove(filePath)
			continue
		}

		fileRecord := File{
			ID:       fileID,
			Name:     fileHeader.Filename,
			Path:     filePath,
			MimeType: contentType,
			Size:     size,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, err = s.db.ExecContext(ctx,
			"INSERT INTO files (id, message_id, name, path, mime_type, size, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			fileRecord.ID, "", fileRecord.Name, fileRecord.Path, fileRecord.MimeType, fileRecord.Size, time.Now(),
		)
		cancel()

		if err != nil {
			log.Printf("Error saving file record: %v", err)
			os.Remove(filePath)
			continue
		}

		uploadedFiles = append(uploadedFiles, fileRecord)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(uploadedFiles)
}

func (s *Server) handleFileServe(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/api/files/")

	var filePath, mimeType string
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := s.db.QueryRowContext(ctx, "SELECT path, mime_type FROM files WHERE id = ?", fileID).Scan(&filePath, &mimeType)
	if err != nil {
		s.sendError(w, "File not found", "NOT_FOUND", http.StatusNotFound)
		return
	}

	// Security: ensure file is within upload directory
	absPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(absPath, filepath.Clean(s.config.UploadDir)) {
		s.sendError(w, "Invalid file path", "INVALID_PATH", http.StatusForbidden)
		return
	}

	w.Header().Set("Content-Type", mimeType)
	http.ServeFile(w, r, filePath)
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	reqBody, _ := json.Marshal(req)
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "POST", s.config.OllamaURL+"/api/generate", bytes.NewBuffer(reqBody))
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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.sendError(w, "Streaming not supported", "STREAMING_ERROR", http.StatusInternalServerError)
		return
	}

	io.Copy(w, resp.Body)
	flusher.Flush()
}

func (s *Server) handleOllamaChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ChatID  string                 `json:"chat_id"`
		Model   string                 `json:"model"`
		Message string                 `json:"message"`
		FileIDs []string               `json:"file_ids"`
		Options map[string]interface{} `json:"options"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Get chat history
	rows, err := s.db.QueryContext(ctx,
		"SELECT role, content FROM messages WHERE chat_id = ? ORDER BY created_at ASC",
		req.ChatID,
	)
	if err != nil {
		log.Printf("Error querying messages: %v", err)
		s.sendError(w, "Failed to load history", "DB_ERROR", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type OllamaMessage struct {
		Role    string   `json:"role"`
		Content string   `json:"content"`
		Images  []string `json:"images,omitempty"`
	}

	var messages []OllamaMessage
	for rows.Next() {
		var msg OllamaMessage
		rows.Scan(&msg.Role, &msg.Content)
		messages = append(messages, msg)
	}
    
    // --- Start: File Context Injection Logic ---
    // Initialize currentMsg before the loop so we can attach images/content
    currentMsg := OllamaMessage{
		Role: "user",
	}

    var fileContext strings.Builder
    
	// Handle image files and gather text files
	for _, fileID := range req.FileIDs {
		var filePath, mimeType, name string
		err := s.db.QueryRowContext(ctx, "SELECT name, path, mime_type FROM files WHERE id = ?", fileID).Scan(&name, &filePath, &mimeType)
		if err != nil {
			log.Printf("Error loading file %s: %v", fileID, err)
			continue
		}

		if strings.HasPrefix(mimeType, "image/") {
			data, err := os.ReadFile(filePath)
			if err == nil {
				encoded := base64.StdEncoding.EncodeToString(data)
				currentMsg.Images = append(currentMsg.Images, encoded)
			}
		} else if strings.HasPrefix(mimeType, "text/") || mimeType == "application/pdf" {
			// Read and include textual content
			data, err := os.ReadFile(filePath)
			if err == nil {
				// Add a clear header for the model
				fileContext.WriteString(fmt.Sprintf("\n--- START FILE: %s (%s) ---\n", name, mimeType))
				fileContext.Write(data)
				fileContext.WriteString(fmt.Sprintf("\n--- END FILE: %s ---\n", name))
			}
		}
	}
    // --- End: File Context Injection Logic ---

	// Finalize content by prepending file context
	currentMsg.Content = fileContext.String() + req.Message
    
	messages = append(messages, currentMsg)

	type OllamaRequest struct {
		Model    string                 `json:"model"`
		Messages []OllamaMessage        `json:"messages"`
		Stream   bool                   `json:"stream"`
		Options  map[string]interface{} `json:"options,omitempty"`
	}

	ollamaReq := OllamaRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   true,
		Options:  req.Options,
	}

	reqBody, _ := json.Marshal(ollamaReq)
	
	chatCtx, chatCancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer chatCancel()

	httpReq, err := http.NewRequestWithContext(chatCtx, "POST", s.config.OllamaURL+"/api/chat", bytes.NewBuffer(reqBody))
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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		s.sendError(w, "Streaming not supported", "STREAMING_ERROR", http.StatusInternalServerError)
		return
	}

	io.Copy(w, resp.Body)
	flusher.Flush()
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, "GET", s.config.OllamaURL+"/api/tags", nil)
	if err != nil {
		s.sendError(w, "Failed to create request", "REQUEST_ERROR", http.StatusInternalServerError)
		return
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		log.Printf("Error calling Ollama: %v", err)
		s.sendError(w, "Failed to connect to Ollama", "OLLAMA_ERROR", http.StatusServiceUnavailable)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, "Invalid request body", "INVALID_REQUEST", http.StatusBadRequest)
		return
	}

	reqBody, _ := json.Marshal(req)
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
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

	w.Header().Set("Content-Type", "text/event-stream")
	io.Copy(w, resp.Body)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
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
	http.ServeFile(w, r, "./index.html")
}

func (s *Server) serveCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	http.ServeFile(w, r, "./style.css")
}

func (s *Server) serveJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	http.ServeFile(w, r, "./script.js")
}