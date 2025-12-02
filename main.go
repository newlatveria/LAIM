package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type Server struct {
	db          *sql.DB
	ollamaURL   string
	uploadDir   string
	sessions    map[string]*Session
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

type OllamaRequest struct {
	Model    string          `json:"model"`
	Messages []OllamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type OllamaMessage struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"`
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server, err := NewServer()
	if err != nil {
		log.Fatal(err)
	}
	defer server.db.Close()

	// Static file routes - must come before other routes
	http.HandleFunc("/style.css", server.serveCSS)
	http.HandleFunc("/script.js", server.serveJS)
	
	// Session endpoints
	http.HandleFunc("/api/session", server.handleSession)
	
	// Chat endpoints
	http.HandleFunc("/api/chats", server.handleChats)
	http.HandleFunc("/api/chats/", server.handleChatDetail)
	http.HandleFunc("/api/messages", server.handleMessages)
	
	// File upload endpoints
	http.HandleFunc("/api/upload", server.handleUpload)
	http.HandleFunc("/api/files/", server.handleFileServe)
	
	// Ollama endpoints
	http.HandleFunc("/api/generate", server.handleGenerate)
	http.HandleFunc("/api/chat", server.handleOllamaChat)
	http.HandleFunc("/api/models", server.handleModels)
	http.HandleFunc("/api/pull", server.handlePull)
	http.HandleFunc("/api/delete", server.handleDelete)

	// Root handler - must be last
	http.HandleFunc("/", server.serveHTML)

	log.Printf("Server starting on http://localhost:%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func NewServer() (*Server, error) {
	db, err := sql.Open("sqlite3", "./laim.db")
	if err != nil {
		return nil, err
	}

	server := &Server{
		db:        db,
		ollamaURL: "http://localhost:11434",
		uploadDir: "./uploads",
		sessions:  make(map[string]*Session),
	}

	if err := server.initDB(); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(server.uploadDir, 0755); err != nil {
		return nil, err
	}

	return server, nil
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
	`

	_, err := s.db.Exec(schema)
	return err
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		sessionID := uuid.New().String()
		session := &Session{
			ID:        sessionID,
			CreatedAt: time.Now(),
			LastSeen:  time.Now(),
		}

		_, err := s.db.Exec(
			"INSERT INTO sessions (id, user_id, created_at, last_seen) VALUES (?, ?, ?, ?)",
			session.ID, session.UserID, session.CreatedAt, session.LastSeen,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		s.sessions[sessionID] = session
		json.NewEncoder(w).Encode(map[string]string{"session_id": sessionID})
	}
}

func (s *Server) handleChats(w http.ResponseWriter, r *http.Request) {
	sessionID := r.Header.Get("X-Session-ID")
	if sessionID == "" {
		http.Error(w, "Session ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "GET":
		rows, err := s.db.Query(
			"SELECT id, session_id, title, model, created_at, updated_at FROM chats WHERE session_id = ? ORDER BY updated_at DESC",
			sessionID,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var chats []Chat
		for rows.Next() {
			var chat Chat
			err := rows.Scan(&chat.ID, &chat.SessionID, &chat.Title, &chat.Model, &chat.CreatedAt, &chat.UpdatedAt)
			if err != nil {
				continue
			}
			chats = append(chats, chat)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chats)

	case "POST":
		var req struct {
			Title string `json:"title"`
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		chat := Chat{
			ID:        uuid.New().String(),
			SessionID: sessionID,
			Title:     req.Title,
			Model:     req.Model,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		_, err := s.db.Exec(
			"INSERT INTO chats (id, session_id, title, model, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			chat.ID, chat.SessionID, chat.Title, chat.Model, chat.CreatedAt, chat.UpdatedAt,
		)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(chat)
	}
}

func (s *Server) handleChatDetail(w http.ResponseWriter, r *http.Request) {
	chatID := strings.TrimPrefix(r.URL.Path, "/api/chats/")
	if chatID == "" {
		http.Error(w, "Chat ID required", http.StatusBadRequest)
		return
	}

	if r.Method == "DELETE" {
		_, err := s.db.Exec("DELETE FROM chats WHERE id = ?", chatID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// GET chat with messages
	rows, err := s.db.Query(
		"SELECT id, chat_id, role, content, created_at FROM messages WHERE chat_id = ? ORDER BY created_at ASC",
		chatID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var msg Message
		err := rows.Scan(&msg.ID, &msg.ChatID, &msg.Role, &msg.Content, &msg.CreatedAt)
		if err != nil {
			continue
		}

		// Get files for this message
		fileRows, err := s.db.Query(
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(messages)
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ChatID  string `json:"chat_id"`
		Role    string `json:"role"`
		Content string `json:"content"`
		FileIDs []string `json:"file_ids"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	msgID := uuid.New().String()
	now := time.Now()

	_, err := s.db.Exec(
		"INSERT INTO messages (id, chat_id, role, content, created_at) VALUES (?, ?, ?, ?, ?)",
		msgID, req.ChatID, req.Role, req.Content, now,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Link files to message
	for _, fileID := range req.FileIDs {
		_, err := s.db.Exec("UPDATE files SET message_id = ? WHERE id = ?", msgID, fileID)
		if err != nil {
			log.Printf("Error linking file %s to message: %v", fileID, err)
		}
	}

	// Update chat timestamp
	_, _ = s.db.Exec("UPDATE chats SET updated_at = ? WHERE id = ?", now, req.ChatID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"id": msgID})
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseMultipartForm(100 << 20); err != nil { // 100MB max
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var uploadedFiles []File

	files := r.MultipartForm.File["files"]
	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			continue
		}
		defer file.Close()

		fileID := uuid.New().String()
		ext := filepath.Ext(fileHeader.Filename)
		filename := fileID + ext
		filePath := filepath.Join(s.uploadDir, filename)

		dst, err := os.Create(filePath)
		if err != nil {
			continue
		}
		defer dst.Close()

		size, err := io.Copy(dst, file)
		if err != nil {
			continue
		}

		fileRecord := File{
			ID:       fileID,
			Name:     fileHeader.Filename,
			Path:     filePath,
			MimeType: fileHeader.Header.Get("Content-Type"),
			Size:     size,
		}

		_, err = s.db.Exec(
			"INSERT INTO files (id, message_id, name, path, mime_type, size, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
			fileRecord.ID, "", fileRecord.Name, fileRecord.Path, fileRecord.MimeType, fileRecord.Size, time.Now(),
		)
		if err != nil {
			continue
		}

		uploadedFiles = append(uploadedFiles, fileRecord)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(uploadedFiles)
}

func (s *Server) handleFileServe(w http.ResponseWriter, r *http.Request) {
	fileID := strings.TrimPrefix(r.URL.Path, "/api/files/")
	
	var filePath string
	err := s.db.QueryRow("SELECT path FROM files WHERE id = ?", fileID).Scan(&filePath)
	if err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, filePath)
}

func (s *Server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reqBody, _ := json.Marshal(req)
	resp, err := http.Post(s.ollamaURL+"/api/generate", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get chat history
	rows, err := s.db.Query(
		"SELECT role, content FROM messages WHERE chat_id = ? ORDER BY created_at ASC",
		req.ChatID,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var messages []OllamaMessage
	for rows.Next() {
		var msg OllamaMessage
		rows.Scan(&msg.Role, &msg.Content)
		messages = append(messages, msg)
	}

	// Add current message with images if any
	currentMsg := OllamaMessage{
		Role:    "user",
		Content: req.Message,
	}

	// Handle image files
	for _, fileID := range req.FileIDs {
		var filePath, mimeType string
		err := s.db.QueryRow("SELECT path, mime_type FROM files WHERE id = ?", fileID).Scan(&filePath, &mimeType)
		if err != nil {
			continue
		}

		if strings.HasPrefix(mimeType, "image/") {
			data, err := os.ReadFile(filePath)
			if err == nil {
				encoded := base64.StdEncoding.EncodeToString(data)
				currentMsg.Images = append(currentMsg.Images, encoded)
			}
		}
	}

	messages = append(messages, currentMsg)

	ollamaReq := OllamaRequest{
		Model:    req.Model,
		Messages: messages,
		Stream:   true,
		Options:  req.Options,
	}

	reqBody, _ := json.Marshal(ollamaReq)
	resp, err := http.Post(s.ollamaURL+"/api/chat", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	io.Copy(w, resp.Body)
	flusher.Flush()
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	resp, err := http.Get(s.ollamaURL + "/api/tags")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}

func (s *Server) handlePull(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reqBody, _ := json.Marshal(req)
	resp, err := http.Post(s.ollamaURL+"/api/pull", "application/json", bytes.NewBuffer(reqBody))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	io.Copy(w, resp.Body)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reqBody, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest("DELETE", s.ollamaURL+"/api/delete", bytes.NewBuffer(reqBody))
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *Server) serveHTML(w http.ResponseWriter, r *http.Request) {
	// Only serve HTML for root path
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	log.Println("Serving HTML")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeFile(w, r, "./index.html")
}

func (s *Server) serveCSS(w http.ResponseWriter, r *http.Request) {
	log.Println("Serving CSS")
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	http.ServeFile(w, r, "./style.css")
}

func (s *Server) serveJS(w http.ResponseWriter, r *http.Request) {
	log.Println("Serving JS")
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	http.ServeFile(w, r, "./script.js")
}