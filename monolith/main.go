package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"code.sajari.com/docconv"
)

////////////////////////////////////////////////////////////////////////////////
// CONFIG
////////////////////////////////////////////////////////////////////////////////

const (
	LOG_FILE_PATH       = "server.log"
	MAX_LOG_SIZE_BYTES  = 10 * 1024 * 1024 // 10 MB
	MAX_LOG_BACKUPS     = 5
	TAIL_MAX_LINES      = 500
	MAX_MULTIPART_MB    = 500
)

////////////////////////////////////////////////////////////////////////////////
// GLOBALS (Logging Live Stream)
////////////////////////////////////////////////////////////////////////////////

var (
	subscribersMu sync.Mutex
	subscribers   = map[chan string]struct{}{}
	tailMu        sync.Mutex
	tailLines     []string
	logFile       *os.File
)

////////////////////////////////////////////////////////////////////////////////
// COLORIZED TERMINAL LOG OUTPUT
////////////////////////////////////////////////////////////////////////////////

type colorWriter struct { w io.Writer }

func (cw colorWriter) Write(p []byte) (int, error) {
	s := string(p)
	prefix := ""
	reset := "\033[0m"

	switch {
	case strings.Contains(s, "[ERROR]"):   prefix = "\033[31m"
	case strings.Contains(s, "[UPLOAD]"):  prefix = "\033[33m"
	case strings.Contains(s, "[OLLAMA]"):  prefix = "\033[36m"
	case strings.Contains(s, "[CHAT]"):    prefix = "\033[35m"
	case strings.Contains(s, "[SESSION]"): prefix = "\033[32m"
	case strings.Contains(s, "[SERVER]"):  prefix = "\033[34m"
	default: prefix = ""
	}

	if prefix != "" {
		p = []byte(prefix + s + reset)
	}

	return cw.w.Write(p)
}

////////////////////////////////////////////////////////////////////////////////
// TRACE ID
////////////////////////////////////////////////////////////////////////////////

func newTraceID() string {
	b := make([]byte, 6)
	_, err := rand.Read(b)
	if err != nil { return fmt.Sprintf("t%d", time.Now().UnixNano()) }
	return hex.EncodeToString(b)
}

func ctxTrace(ctx context.Context) string {
	if ctx == nil { return "none" }
	v := ctx.Value("trace")
	if v == nil { return "none" }
	if s, ok := v.(string); ok { return s }
	return "none"
}

////////////////////////////////////////////////////////////////////////////////
// LOGGING HELPERS
////////////////////////////////////////////////////////////////////////////////

func L(r *http.Request, lvl, msg string, v ...interface{}) {
	trace := "static"
	if r != nil { trace = ctxTrace(r.Context()) }
	line := fmt.Sprintf("[%s] [trace=%s] %s", lvl, trace, fmt.Sprintf(msg, v...))
	log.Println(line)
	broadcast(line)
}

func broadcast(line string) {
	tailMu.Lock()
	tailLines = append(tailLines, line)
	if len(tailLines) > TAIL_MAX_LINES {
		tailLines = tailLines[len(tailLines)-TAIL_MAX_LINES:]
	}
	tailMu.Unlock()

	subscribersMu.Lock()
	for ch := range subscribers {
		select {
		case ch <- line:
		default:
		}
	}
	subscribersMu.Unlock()
}

////////////////////////////////////////////////////////////////////////////////
// LOG ROTATION
////////////////////////////////////////////////////////////////////////////////

func rotateIfNeeded() {
	fi, err := os.Stat(LOG_FILE_PATH)
	if err != nil { return }
	if fi.Size() < MAX_LOG_SIZE_BYTES { return }

	if logFile != nil { logFile.Close() }

	for i := MAX_LOG_BACKUPS - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", LOG_FILE_PATH, i)
		to := fmt.Sprintf("%s.%d", LOG_FILE_PATH, i+1)
		if _, err := os.Stat(from); err == nil {
			os.Rename(from, to)
		}
	}

	os.Rename(LOG_FILE_PATH, fmt.Sprintf("%s.1", LOG_FILE_PATH))

	f, err := os.OpenFile(LOG_FILE_PATH, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.SetOutput(colorWriter{os.Stdout})
		return
	}
	logFile = f

	multi := io.MultiWriter(colorWriter{os.Stdout}, logFile)
	log.SetOutput(multi)
}

func startRotation() {
	go func() {
		t := time.NewTicker(15 * time.Second)
		for range t.C {
			rotateIfNeeded()
		}
	}()
}

////////////////////////////////////////////////////////////////////////////////
// MIDDLEWARE — TRACE + TIMING
////////////////////////////////////////////////////////////////////////////////

func withTraceTiming(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		trace := newTraceID()
		r = r.WithContext(context.WithValue(r.Context(), "trace", trace))

		start := time.Now()
		L(r, "INFO", "Incoming %s %s", r.Method, r.URL.Path)

		next(w, r)

		L(r, "INFO", "Completed in %dms", time.Since(start).Milliseconds())
	}
}

////////////////////////////////////////////////////////////////////////////////
// CONFIG
////////////////////////////////////////////////////////////////////////////////

type Config struct {
	Port         string
	OllamaURL    string
	DBPath       string
	MaxUpload    int64
}

func loadConfig() Config {
	return Config{
		Port:       getenv("PORT", "8080"),
		OllamaURL:  getenv("OLLAMA_URL", "http://localhost:11434"),
		DBPath:     getenv("DATABASE_PATH", "./laim.db"),
		MaxUpload:  10 << 20,
	}
}

func getenv(k, def string) string {
	v := os.Getenv(k)
	if v == "" { return def }
	return v
}

////////////////////////////////////////////////////////////////////////////////
// DATABASE
////////////////////////////////////////////////////////////////////////////////

type Server struct {
	db  *sql.DB
	cfg Config
}

func initDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil { return nil, err }

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
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  chat_id TEXT,
  role TEXT,
  content TEXT,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
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
	return db, err
}

////////////////////////////////////////////////////////////////////////////////
// SSE LOG STREAM HANDLERS
////////////////////////////////////////////////////////////////////////////////

func logsPage(w http.ResponseWriter, r *http.Request) {
	t := template.Must(template.New("logs").Parse(`
<!doctype html><html><body>
<h2>Live Logs</h2>
<pre id="log"></pre>
<script>
let log = document.getElementById("log");
let s = new EventSource("/logs/stream");
s.onmessage = e => {
  log.textContent += e.data + "\n";
  log.scrollTop = log.scrollHeight;
};
</script>
</body></html>
`))
	t.Execute(w, nil)
}

func logsStream(w http.ResponseWriter, r *http.Request) {
	f, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", 500)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	tailMu.Lock()
	initial := append([]string{}, tailLines...)
	tailMu.Unlock()

	for _, ln := range initial {
		fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(ln, "\n", "\\n"))
	}
	f.Flush()

	ch := make(chan string, 100)
	subscribersMu.Lock()
	subscribers[ch] = struct{}{}
	subscribersMu.Unlock()

	defer func() {
		subscribersMu.Lock()
		delete(subscribers, ch)
		close(ch)
		subscribersMu.Unlock()
	}()

	notify := r.Context().Done()
	for {
		select {
		case line := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", strings.ReplaceAll(line, "\n", "\\n"))
			f.Flush()
		case <-notify:
			return
		}
	}
}

////////////////////////////////////////////////////////////////////////////////
// FILE PROCESSING
////////////////////////////////////////////////////////////////////////////////

type File struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	Content  string `json:"content"` // DataURI base64
}

func isBinary(b []byte) bool {
	if len(b) > 512 { b = b[:512] }
	if bytes.Contains(b, []byte{0}) { return true }
	return !utf8.Valid(b)
}

func processMultipartFile(fh *multipart.FileHeader) (File, error) {
	fp, err := fh.Open()
	if err != nil { return File{}, err }
	defer fp.Close()

	all, err := io.ReadAll(fp)
	if err != nil { return File{}, err }

	mime := http.DetectContentType(all)
	ext := strings.ToLower(filepath.Ext(fh.Filename))

	out := File{Name: fh.Filename, MimeType: mime}

	if strings.HasPrefix(mime, "image/") {
		out.Content = "data:" + mime + ";base64," +
			base64.StdEncoding.EncodeToString(all)
		return out, nil
	}

	switch ext {
	case ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx", ".html", ".rtf":
		res, err := docconv.Convert(bytes.NewReader(all), ext, false)
		if err == nil {
			out.MimeType = "text/plain"
			out.Content = "data:text/plain;base64," +
				base64.StdEncoding.EncodeToString([]byte(res.Body))
			return out, nil
		}
	}

	if isBinary(all) {
		out.Content = "data:application/octet-stream;base64," +
			base64.StdEncoding.EncodeToString(all)
	} else {
		out.MimeType = "text/plain"
		out.Content = "data:text/plain;base64," +
			base64.StdEncoding.EncodeToString(all)
	}
	return out, nil
}

////////////////////////////////////////////////////////////////////////////////
// API — SAME AS ORIGINAL (PRESERVED)
////////////////////////////////////////////////////////////////////////////////

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

////////////////////////////////////////////////////////////////////////////////
// SERVER METHODS — CHATS
////////////////////////////////////////////////////////////////////////////////

func (s *Server) createChat(sessionID, model string) (*Chat, error) {
	id := uuid.New().String()
	now := time.Now()

	_, err := s.db.Exec(`
		INSERT INTO chats (id, session_id, title, model, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, sessionID, "New Chat", model, now, now)

	if err != nil {
		return nil, err
	}

	return &Chat{
		ID:        id,
		SessionID: sessionID,
		Title:     "New Chat",
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Server) getChatsBySession(sessionID string) ([]Chat, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, title, model, created_at, updated_at
		FROM chats
		WHERE session_id = ?
		ORDER BY updated_at DESC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Chat
	for rows.Next() {
		var c Chat
		if err := rows.Scan(
			&c.ID, &c.SessionID, &c.Title, &c.Model,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, nil
}

func (s *Server) getChat(id string) (*Chat, error) {
	var c Chat
	err := s.db.QueryRow(`
		SELECT id, session_id, title, model, created_at, updated_at
		FROM chats WHERE id = ?
	`, id).Scan(
		&c.ID, &c.SessionID, &c.Title, &c.Model,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

////////////////////////////////////////////////////////////////////////////////
// SERVER METHODS — MESSAGES
////////////////////////////////////////////////////////////////////////////////

func (s *Server) getMessages(chatID string) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, role, content, created_at
		FROM messages
		WHERE chat_id = ?
		ORDER BY created_at ASC
	`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(
			&m.ID, &m.ChatID, &m.Role, &m.Content, &m.CreatedAt,
		); err != nil {
			return nil, err
		}

		files, _ := s.getFiles(m.ID)
		m.Files = files
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (s *Server) createMessage(chatID, role, content string, files []File) (*Message, error) {
	id := uuid.New().String()
	now := time.Now()

	_, err := s.db.Exec(`
		INSERT INTO messages (id, chat_id, role, content, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, chatID, role, content, now)
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		_, err := s.db.Exec(`
			INSERT INTO files (id, message_id, name, mime_type, content_base64)
			VALUES (?, ?, ?, ?, ?)
		`, uuid.New().String(), id, f.Name, f.MimeType, f.Content)
		if err != nil {
			return nil, err
		}
	}

	_, _ = s.db.Exec(`UPDATE chats SET updated_at = ? WHERE id = ?`, now, chatID)

	return &Message{
		ID:        id,
		ChatID:    chatID,
		Role:      role,
		Content:   content,
		Files:     files,
		CreatedAt: now,
	}, nil
}

////////////////////////////////////////////////////////////////////////////////
// SERVER METHODS — FILES
////////////////////////////////////////////////////////////////////////////////

func (s *Server) getFiles(messageID string) ([]File, error) {
	rows, err := s.db.Query(`
		SELECT name, mime_type, content_base64
		FROM files
		WHERE message_id = ?
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.Name, &f.MimeType, &f.Content); err != nil {
			return nil, err
		}
		items = append(items, f)
	}
	return items, nil
}

////////////////////////////////////////////////////////////////////////////////
// OLLAMA STREAMING (Preserved)
////////////////////////////////////////////////////////////////////////////////

type OllamaClient struct {
	baseURL string
}

func newOllamaClient(base string) *OllamaClient {
	return &OllamaClient{baseURL: base}
}

func (o *OllamaClient) ChatStream(model string, msgs []map[string]string, fn func(string)) error {
	payload := map[string]interface{}{
		"model":    model,
		"messages": msgs,
		"stream":   true,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil { return err }
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil { return err }

		text := strings.TrimSpace(string(line))
		if text == "" { continue }

		fn(text)
	}
}

////////////////////////////////////////////////////////////////////////////////
// UPLOAD ENDPOINT (Preserved + Fixed)
////////////////////////////////////////////////////////////////////////////////

func (s *Server) uploadHandler(w http.ResponseWriter, r *http.Request) {
	L(r, "UPLOAD", "Starting upload")

	err := r.ParseMultipartForm(MAX_MULTIPART_MB << 20)
	if err != nil {
		L(r, "ERROR", "ParseMultipartForm: %v", err)
		http.Error(w, err.Error(), 400)
		return
	}

	chatID := r.FormValue("chat_id")
	model := r.FormValue("model")
	prompt := r.FormValue("prompt")

	var fileObjs []File
	fhs := r.MultipartForm.File["files"]
	for _, fh := range fhs {
		L(r, "UPLOAD", "Processing %s", fh.Filename)
		fobj, err := processMultipartFile(fh)
		if err != nil {
			L(r, "ERROR", "process file: %v", err)
			continue
		}
		fileObjs = append(fileObjs, fobj)
	}

	msg, err := s.createMessage(chatID, "user", prompt, fileObjs)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"message_id": msg.ID,
	})
}

////////////////////////////////////////////////////////////////////////////////
// CHAT COMPLETION / STREAM
////////////////////////////////////////////////////////////////////////////////

func (s *Server) chatStreamHandler(w http.ResponseWriter, r *http.Request) {
	L(r, "CHAT", "Streaming request received")

	var req struct {
		ChatID string `json:"chat_id"`
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	msgs, err := s.getMessages(req.ChatID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var conv []map[string]string
	for _, m := range msgs {
		conv = append(conv, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	conv = append(conv, map[string]string{
		"role":    "user",
		"content": req.Prompt,
	})

	o := newOllamaClient(s.cfg.OllamaURL)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	var buffer strings.Builder

	err = o.ChatStream(req.Model, conv, func(chunk string) {
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()

		buffer.WriteString(chunk)
	})
	if err != nil {
		L(r, "ERROR", "ollama stream: %v", err)
	}

	_, err = s.createMessage(req.ChatID, "assistant", buffer.String(), nil)
	if err != nil {
		L(r, "ERROR", "saving assistant msg: %v", err)
	}
}

////////////////////////////////////////////////////////////////////////////////
// STATIC FILE HANDLERS (Preserved)
////////////////////////////////////////////////////////////////////////////////

func serveFile(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}

////////////////////////////////////////////////////////////////////////////////
// SERVER METHODS — CHATS
////////////////////////////////////////////////////////////////////////////////

func (s *Server) createChat(sessionID, model string) (*Chat, error) {
	id := uuid.New().String()
	now := time.Now()

	_, err := s.db.Exec(`
		INSERT INTO chats (id, session_id, title, model, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, sessionID, "New Chat", model, now, now)

	if err != nil {
		return nil, err
	}

	return &Chat{
		ID:        id,
		SessionID: sessionID,
		Title:     "New Chat",
		Model:     model,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *Server) getChatsBySession(sessionID string) ([]Chat, error) {
	rows, err := s.db.Query(`
		SELECT id, session_id, title, model, created_at, updated_at
		FROM chats
		WHERE session_id = ?
		ORDER BY updated_at DESC
	`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []Chat
	for rows.Next() {
		var c Chat
		if err := rows.Scan(
			&c.ID, &c.SessionID, &c.Title, &c.Model,
			&c.CreatedAt, &c.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, nil
}

func (s *Server) getChat(id string) (*Chat, error) {
	var c Chat
	err := s.db.QueryRow(`
		SELECT id, session_id, title, model, created_at, updated_at
		FROM chats WHERE id = ?
	`, id).Scan(
		&c.ID, &c.SessionID, &c.Title, &c.Model,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

////////////////////////////////////////////////////////////////////////////////
// SERVER METHODS — MESSAGES
////////////////////////////////////////////////////////////////////////////////

func (s *Server) getMessages(chatID string) ([]Message, error) {
	rows, err := s.db.Query(`
		SELECT id, chat_id, role, content, created_at
		FROM messages
		WHERE chat_id = ?
		ORDER BY created_at ASC
	`, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(
			&m.ID, &m.ChatID, &m.Role, &m.Content, &m.CreatedAt,
		); err != nil {
			return nil, err
		}

		files, _ := s.getFiles(m.ID)
		m.Files = files
		msgs = append(msgs, m)
	}
	return msgs, nil
}

func (s *Server) createMessage(chatID, role, content string, files []File) (*Message, error) {
	id := uuid.New().String()
	now := time.Now()

	_, err := s.db.Exec(`
		INSERT INTO messages (id, chat_id, role, content, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, chatID, role, content, now)
	if err != nil {
		return nil, err
	}

	for _, f := range files {
		_, err := s.db.Exec(`
			INSERT INTO files (id, message_id, name, mime_type, content_base64)
			VALUES (?, ?, ?, ?, ?)
		`, uuid.New().String(), id, f.Name, f.MimeType, f.Content)
		if err != nil {
			return nil, err
		}
	}

	_, _ = s.db.Exec(`UPDATE chats SET updated_at = ? WHERE id = ?`, now, chatID)

	return &Message{
		ID:        id,
		ChatID:    chatID,
		Role:      role,
		Content:   content,
		Files:     files,
		CreatedAt: now,
	}, nil
}

////////////////////////////////////////////////////////////////////////////////
// SERVER METHODS — FILES
////////////////////////////////////////////////////////////////////////////////

func (s *Server) getFiles(messageID string) ([]File, error) {
	rows, err := s.db.Query(`
		SELECT name, mime_type, content_base64
		FROM files
		WHERE message_id = ?
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.Name, &f.MimeType, &f.Content); err != nil {
			return nil, err
		}
		items = append(items, f)
	}
	return items, nil
}

////////////////////////////////////////////////////////////////////////////////
// OLLAMA STREAMING (Preserved)
////////////////////////////////////////////////////////////////////////////////

type OllamaClient struct {
	baseURL string
}

func newOllamaClient(base string) *OllamaClient {
	return &OllamaClient{baseURL: base}
}

func (o *OllamaClient) ChatStream(model string, msgs []map[string]string, fn func(string)) error {
	payload := map[string]interface{}{
		"model":    model,
		"messages": msgs,
		"stream":   true,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequest("POST", o.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil { return err }
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil { return err }
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil { return err }

		text := strings.TrimSpace(string(line))
		if text == "" { continue }

		fn(text)
	}
}

////////////////////////////////////////////////////////////////////////////////
// UPLOAD ENDPOINT (Preserved + Fixed)
////////////////////////////////////////////////////////////////////////////////

func (s *Server) uploadHandler(w http.ResponseWriter, r *http.Request) {
	L(r, "UPLOAD", "Starting upload")

	err := r.ParseMultipartForm(MAX_MULTIPART_MB << 20)
	if err != nil {
		L(r, "ERROR", "ParseMultipartForm: %v", err)
		http.Error(w, err.Error(), 400)
		return
	}

	chatID := r.FormValue("chat_id")
	model := r.FormValue("model")
	prompt := r.FormValue("prompt")

	var fileObjs []File
	fhs := r.MultipartForm.File["files"]
	for _, fh := range fhs {
		L(r, "UPLOAD", "Processing %s", fh.Filename)
		fobj, err := processMultipartFile(fh)
		if err != nil {
			L(r, "ERROR", "process file: %v", err)
			continue
		}
		fileObjs = append(fileObjs, fobj)
	}

	msg, err := s.createMessage(chatID, "user", prompt, fileObjs)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"message_id": msg.ID,
	})
}

////////////////////////////////////////////////////////////////////////////////
// CHAT COMPLETION / STREAM
////////////////////////////////////////////////////////////////////////////////

func (s *Server) chatStreamHandler(w http.ResponseWriter, r *http.Request) {
	L(r, "CHAT", "Streaming request received")

	var req struct {
		ChatID string `json:"chat_id"`
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	msgs, err := s.getMessages(req.ChatID)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	var conv []map[string]string
	for _, m := range msgs {
		conv = append(conv, map[string]string{
			"role":    m.Role,
			"content": m.Content,
		})
	}

	conv = append(conv, map[string]string{
		"role":    "user",
		"content": req.Prompt,
	})

	o := newOllamaClient(s.cfg.OllamaURL)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)

	var buffer strings.Builder

	err = o.ChatStream(req.Model, conv, func(chunk string) {
		fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()

		buffer.WriteString(chunk)
	})
	if err != nil {
		L(r, "ERROR", "ollama stream: %v", err)
	}

	_, err = s.createMessage(req.ChatID, "assistant", buffer.String(), nil)
	if err != nil {
		L(r, "ERROR", "saving assistant msg: %v", err)
	}
}

////////////////////////////////////////////////////////////////////////////////
// STATIC FILE HANDLERS (Preserved)
////////////////////////////////////////////////////////////////////////////////

func serveFile(path string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}

