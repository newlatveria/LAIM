package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

	"github.com/ollama/ollama/api"
	"code.sajari.com/docconv"
)

/* ---------------------------------------------------------
   CONFIG
--------------------------------------------------------- */

const (
	logFilePath       = "server.log"
	maxLogSizeBytes   = 10 * 1024 * 1024 // 10 MB
	maxLogBackups     = 5
	tailMaxLinesConst = 500
	maxUploadMB       = 500 // for ParseMultipartForm (in MB)
)

/* ---------------------------------------------------------
   LOG BROADCASTER + TAIL CACHE (SSE)
--------------------------------------------------------- */

var (
	subscribersMu sync.Mutex
	subscribers   = map[chan string]struct{}{}
	tailMu        sync.Mutex
	tailLines     []string
	tailMaxLines  = tailMaxLinesConst

	// log rotation synchronization
	logRotateMu sync.Mutex
	logFile     *os.File
)

/* append to tail and broadcast */
func broadcastLine(line string) {
	tailMu.Lock()
	tailLines = append(tailLines, line)
	if len(tailLines) > tailMaxLines {
		tailLines = tailLines[len(tailLines)-tailMaxLines:]
	}
	tailMu.Unlock()

	subscribersMu.Lock()
	for ch := range subscribers {
		select {
		case ch <- line:
		default:
			// slow client; skip
		}
	}
	subscribersMu.Unlock()
}

/* preload existing log file into tail at startup */
func preloadTail() {
	data, err := ioutil.ReadFile(logFilePath)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > tailMaxLines {
		lines = lines[len(lines)-tailMaxLines:]
	}
	tailMu.Lock()
	for _, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			tailLines = append(tailLines, ln)
		}
	}
	tailMu.Unlock()
}

/* ---------------------------------------------------------
   COLOR WRITER (terminal) - file logs remain plain
--------------------------------------------------------- */

type colorWriter struct {
	w io.Writer
}

func (cw colorWriter) Write(p []byte) (n int, err error) {
	s := string(p)
	col := ""
	reset := "\033[0m"

	switch {
	case strings.Contains(s, "[ERROR]"):
		col = "\033[31m" // red
	case strings.Contains(s, "[UPLOAD]"):
		col = "\033[33m" // yellow
	case strings.Contains(s, "[OLLAMA]"):
		col = "\033[36m" // cyan
	case strings.Contains(s, "[CHAT]"):
		col = "\033[35m" // magenta
	case strings.Contains(s, "[SESSION]"):
		col = "\033[32m" // green
	case strings.Contains(s, "[MODELS]"):
		col = "\033[34m" // blue
	default:
		col = ""
	}

	if col != "" {
		p = []byte(col + s + reset)
	}
	return cw.w.Write(p)
}

/* ---------------------------------------------------------
   TRACE ID
--------------------------------------------------------- */

func newTraceID() string {
	b := make([]byte, 6)
	_, err := rand.Read(b)
	if err != nil {
		return fmt.Sprintf("t%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func ctxTrace(ctx context.Context) string {
	if ctx == nil {
		return "none"
	}
	if v := ctx.Value("trace"); v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "none"
}

/* ---------------------------------------------------------
   LOGGING HELPERS
--------------------------------------------------------- */

func Ln(level, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	final := fmt.Sprintf("[%s] [trace=%s] %s", level, "static", msg)
	log.Println(final)
	broadcastLine(final)
}

func L(r *http.Request, level, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	trace := "none"
	if r != nil {
		trace = ctxTrace(r.Context())
	}
	final := fmt.Sprintf("[%s] [trace=%s] %s", level, trace, msg)
	log.Println(final)
	broadcastLine(final)
}

/* ---------------------------------------------------------
   LOG ROTATION
   Periodically checks size and rotates if > threshold.
   Keeps maxLogBackups backups named server.log.1 .. server.log.N
--------------------------------------------------------- */

func rotateLogsIfNeededPeriodically() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		rotateIfNeeded()
	}
}

func rotateIfNeeded() {
	logRotateMu.Lock()
	defer logRotateMu.Unlock()

	fi, err := os.Stat(logFilePath)
	if err != nil {
		// maybe not exists
		return
	}
	if fi.Size() < maxLogSizeBytes {
		return
	}

	// close current log file if open
	if logFile != nil {
		_ = logFile.Close()
	}

	// rotate backups: server.log.(maxBackups-1) -> server.log.maxBackups, etc.
	for i := maxLogBackups - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", logFilePath, i)
		to := fmt.Sprintf("%s.%d", logFilePath, i+1)
		if _, err := os.Stat(from); err == nil {
			_ = os.Rename(from, to)
		}
	}
	// rotate current to .1
	_ = os.Rename(logFilePath, fmt.Sprintf("%s.1", logFilePath))

	// reopen new file
	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// fallback: write to stdout only
		fmt.Fprintf(os.Stderr, "rotate failed: %v\n", err)
		log.SetOutput(colorWriter{os.Stdout})
		return
	}
	logFile = f
	// reset logger output to multiwriter (terminal colors + file)
	multi := io.MultiWriter(colorWriter{os.Stdout}, logFile)
	log.SetOutput(multi)
	Ln("INFO", "Rotated logs; new log file created")
}

/* ---------------------------------------------------------
   SESSION STORAGE
--------------------------------------------------------- */

var sessionHistory = struct {
	sync.Mutex
	H map[string][]api.Message
}{H: make(map[string][]api.Message)}

/* ---------------------------------------------------------
   HELPERS (session, binary detection)
--------------------------------------------------------- */

func boolPtr(b bool) *bool { return &b }

func getSessionID(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie("session_id")
	if err == nil && c.Value != "" {
		return c.Value
	}

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    id,
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: false,
	})

	L(r, "SESSION", "Created new session: %s", id)
	return id
}

func isBinary(b []byte) bool {
	check := b
	if len(check) > 512 {
		check = b[:512]
	}
	if strings.Contains(string(check), "\x00") {
		return true
	}
	return !utf8.Valid(check)
}

/* ---------------------------------------------------------
   FILE PROCESSING
--------------------------------------------------------- */

type OllamaInput struct {
	TextContent string
	ImageBytes  [][]byte
	Filename    string
	MimeType    string
}

func ProcessSingleFile(file multipart.File, header *multipart.FileHeader, r *http.Request) (OllamaInput, error) {
	L(r, "UPLOAD", "Processing file: %s (size=%d)", header.Filename, header.Size)

	all, err := io.ReadAll(file)
	if err != nil {
		L(r, "ERROR", "Failed reading file %s: %v", header.Filename, err)
		return OllamaInput{}, err
	}

	mime := http.DetectContentType(all)
	ext := strings.ToLower(filepath.Ext(header.Filename))

	L(r, "UPLOAD", "Detected MIME=%s EXT=%s", mime, ext)

	out := OllamaInput{
		Filename: header.Filename,
		MimeType: mime,
	}

	if strings.HasPrefix(mime, "image/") {
		L(r, "UPLOAD", "%s recognized as image (%d bytes)", header.Filename, len(all))
		out.ImageBytes = [][]byte{all}
		return out, nil
	}

	switch ext {
	case ".pdf", ".docx", ".doc", ".xlsx", ".xls", ".pptx", ".ppt", ".odt", ".rtf", ".html":
		L(r, "UPLOAD", "Sending %s to docconv", header.Filename)
		res, err := docconv.Convert(strings.NewReader(string(all)), ext, false)
		if err == nil {
			L(r, "UPLOAD", "docconv succeeded for %s (%d chars extracted)", header.Filename, len(res.Body))
			out.TextContent = res.Body
			return out, nil
		}
		L(r, "ERROR", "docconv failed for %s: %v", header.Filename, err)
	}

	if isBinary(all) {
		L(r, "UPLOAD", "File %s appears binary", header.Filename)
		out.TextContent = "[WARNING: binary file]\n" + string(all)
	} else {
		L(r, "UPLOAD", "Treating %s as raw text (%d chars)", header.Filename, len(all))
		out.TextContent = string(all)
	}

	return out, nil
}

/* ---------------------------------------------------------
   PAGE DATA + TEMPLATE RENDERING
--------------------------------------------------------- */

type PageData struct {
	Error         string
	Filenames     []string
	ExtractedText string
	OllamaOutput  string
	Models        []string
	History       []api.Message
	Trace         string
}

var tmpl = template.Must(template.New("layout").Parse(htmlTemplate))

func renderTemplate(w http.ResponseWriter, r *http.Request, data PageData) {
	if len(data.ExtractedText) > 3000 {
		data.ExtractedText = data.ExtractedText[:3000] + "...\n(Truncated)"
	}
	if r != nil {
		data.Trace = ctxTrace(r.Context())
	}
	_ = tmpl.Execute(w, data)
}

/* ---------------------------------------------------------
   OLLAMA CALL
--------------------------------------------------------- */

func callOllama(model string, msgs []api.Message, r *http.Request) (string, error) {
	client := api.NewClient(&url.URL{Scheme: "http", Host: "localhost:11434"}, http.DefaultClient)

	req := &api.ChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   boolPtr(true),
	}

	L(r, "OLLAMA", "Model=%s | Sending prompt (%d messages)", model, len(msgs))

	var out strings.Builder

	err := client.Chat(context.Background(), req, func(resp api.ChatResponse) error {
		out.WriteString(resp.Message.Content)
		return nil
	})

	L(r, "OLLAMA", "Response size=%d chars", out.Len())
	return out.String(), err
}

/* ---------------------------------------------------------
   MODEL LISTING
--------------------------------------------------------- */

func listLocalModels(r *http.Request) ([]string, error) {
	client := api.NewClient(&url.URL{Scheme: "http", Host: "localhost:11434"}, http.DefaultClient)
	res, err := client.List(context.Background())
	if err != nil {
		L(r, "ERROR", "Failed listing models: %v", err)
		return nil, err
	}
	var names []string
	for _, m := range res.Models {
		names = append(names, m.Name)
	}
	L(r, "MODELS", "Found %d models", len(names))
	return names, nil
}

/* ---------------------------------------------------------
   MIDDLEWARE: withTraceAndTiming
   Sets trace id and logs request timing after handler returns
--------------------------------------------------------- */

func withTraceAndTiming(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		trace := newTraceID()
		ctx := context.WithValue(r.Context(), "trace", trace)
		r = r.WithContext(ctx)

		start := time.Now()
		L(r, "INFO", "Incoming %s %s", r.Method, r.URL.Path)

		// call handler
		next(w, r)

		// after handler returns
		elapsed := time.Since(start)
		L(r, "INFO", "Request finished in %d ms", elapsed.Milliseconds())
	}
}

/* ---------------------------------------------------------
   HANDLERS
--------------------------------------------------------- */

func homeHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(w, r)
	L(r, "HOME", "Session=%s opened homepage", sessionID)

	models, _ := listLocalModels(r)

	sessionHistory.Lock()
	h := sessionHistory.H[sessionID]
	sessionHistory.Unlock()

	renderTemplate(w, r, PageData{
		Models:  models,
		History: h,
	})
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(w, r)

	msg := r.FormValue("user_prompt")
	model := r.FormValue("model_select")

	L(r, "CHAT", "Session=%s Model=%s PromptSize=%d", sessionID, model, len(msg))

	if strings.TrimSpace(msg) == "" {
		L(r, "CHAT", "Empty message — ignored")
		http.Redirect(w, r, "/", 303)
		return
	}

	sessionHistory.Lock()
	h := sessionHistory.H[sessionID]
	sessionHistory.Unlock()

	h = append(h, api.Message{Role: "user", Content: msg})

	output, err := callOllama(model, h, r)
	if err != nil {
		L(r, "ERROR", "Ollama call failed: %v", err)
		// If XHR, respond with error text so client can replace waiting
		if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderTemplate(w, r, PageData{Error: err.Error()})
		return
	}

	h = append(h, api.Message{Role: "assistant", Content: output})

	sessionHistory.Lock()
	sessionHistory.H[sessionID] = h
	sessionHistory.Unlock()

	L(r, "CHAT", "AI returned %d chars", len(output))

	// If AJAX, return assistant content only
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, output)
		return
	}

	http.Redirect(w, r, "/", 303)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(w, r)
	L(r, "UPLOAD", "Start upload for session=%s", sessionID)

	err := r.ParseMultipartForm(maxUploadMB << 20)
	if err != nil {
		L(r, "ERROR", "ParseMultipartForm: %v", err)
		renderTemplate(w, r, PageData{Error: err.Error()})
		return
	}

	userPrompt := r.FormValue("user_prompt")
	model := r.FormValue("model_select")

	files := r.MultipartForm.File["file_upload"]
	L(r, "UPLOAD", "%d files detected", len(files))

	if len(files) == 0 {
		L(r, "UPLOAD", "No files found in request")
		renderTemplate(w, r, PageData{Error: "No files uploaded."})
		return
	}

	var text strings.Builder
	var images []api.ImageData
	var names []string

	for _, fh := range files {
		L(r, "UPLOAD", "Handling: %s (%d bytes)", fh.Filename, fh.Size)

		f, _ := fh.Open()
		data, err := ProcessSingleFile(f, fh, r)
		f.Close()

		if err != nil {
			L(r, "ERROR", "Processing file %s: %v", fh.Filename, err)
			continue
		}

		names = append(names, fh.Filename)

		if data.TextContent != "" {
			text.WriteString("\n--- " + fh.Filename + " ---\n")
			text.WriteString(data.TextContent + "\n")
		}

		for _, img := range data.ImageBytes {
			images = append(images, api.ImageData(img))
		}
	}

	sessionHistory.Lock()
	h := sessionHistory.H[sessionID]
	sessionHistory.Unlock()

	fullPrompt := fmt.Sprintf("%s\n\n[FILES]\n%s", userPrompt, text.String())
	L(r, "UPLOAD", "Sending combined prompt (%d chars, %d images)", len(fullPrompt), len(images))

	h = append(h, api.Message{
		Role:    "user",
		Content: fullPrompt,
		Images:  images,
	})

	output, err := callOllama(model, h, r)
	if err != nil {
		L(r, "ERROR", "Ollama upload call failed: %v", err)
		renderTemplate(w, r, PageData{Error: err.Error()})
		return
	}

	h = append(h, api.Message{Role: "assistant", Content: output})

	sessionHistory.Lock()
	sessionHistory.H[sessionID] = h
	sessionHistory.Unlock()

	L(r, "UPLOAD", "Ollama returned %d chars", len(output))

	models, _ := listLocalModels(r)

	// For XHR uploads, respond lightly
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "OK\nProcessed files: %d\n", len(names))
		return
	}

	renderTemplate(w, r, PageData{
		Filenames:     names,
		ExtractedText: text.String(),
		OllamaOutput:  output,
		Models:        models,
		History:       h,
	})
}

/* ---------------------------------------------------------
   LOGS SSE endpoints and logs page
--------------------------------------------------------- */

func logsSSEHandler(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// initial tail
	tailMu.Lock()
	initial := make([]string, len(tailLines))
	copy(initial, tailLines)
	tailMu.Unlock()

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

	for _, line := range initial {
		fmt.Fprintf(w, "data: %s\n\n", escapeForSSE(line))
	}
	flusher.Flush()

	notify := w.(http.CloseNotifier).CloseNotify()

	for {
		select {
		case line := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", escapeForSSE(line))
			flusher.Flush()
		case <-notify:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func logsPageHandler(w http.ResponseWriter, r *http.Request) {
	t := template.Must(template.New("logs").Parse(logsHTML))
	_ = t.Execute(w, nil)
}

func escapeForSSE(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

/* ---------------------------------------------------------
   MAIN
--------------------------------------------------------- */

func main() {
	// open log file
	f, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}
	logFile = f

	// multiwriter: colored terminal + plain file
	multi := io.MultiWriter(colorWriter{os.Stdout}, logFile)
	log.SetOutput(multi)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	// preload tail
	preloadTail()

	Ln("INFO", "=== Server starting ===")
	Ln("INFO", "Logging to %s and terminal", logFilePath)

	// start rotation checker
	go rotateLogsIfNeededPeriodically()

	// routes with middleware that sets trace + timing
	http.HandleFunc("/", withTraceAndTiming(homeHandler))
	http.HandleFunc("/upload", withTraceAndTiming(uploadHandler))
	http.HandleFunc("/chat", withTraceAndTiming(chatHandler))
	http.HandleFunc("/logs", withTraceAndTiming(logsPageHandler))
	http.HandleFunc("/logs/stream", withTraceAndTiming(logsSSEHandler))

	Ln("SERVER", "Server running at http://localhost:8080")
	fmt.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

/* ---------------------------------------------------------
   HTML TEMPLATES: main + logs
   The main template now:
   - chat uses AJAX and shows "Awaiting model response..."
   - retains file/folder upload + drag-drop + progress
--------------------------------------------------------- */

const htmlTemplate = `
<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
<meta charset="UTF-8" />
<title>Ollama Universal File Loader + Chat</title>
<meta name="viewport" content="width=device-width, initial-scale=1" />
<style>
:root {
  --bg: #111; --fg: #eaeaea; --accent:#3ea6ff; --card:#1e1e1e; --border:#333; --muted:#888;
}
html[data-theme=light] { --bg:#fafafa; --fg:#222; --accent:#0066cc; --card:#fff; --border:#ccc; --muted:#666; }
body { font-family: system-ui, sans-serif; background:var(--bg); color:var(--fg); margin:0; padding:20px; }
.container { max-width:900px; margin:auto; background:var(--card); padding:20px; border-radius:12px; border:1px solid var(--border); }
textarea, select, input[type=file] { width:100%; padding:10px; border-radius:6px; background:var(--bg); color:var(--fg); border:1px solid var(--border); }
button, input[type=submit] { width:100%; padding:12px; background:var(--accent); border:none; border-radius:6px; cursor:pointer; margin-top:10px; color:#000; font-size:1rem; }
.result { background:var(--bg); border:1px solid var(--border); padding:12px; white-space:pre-wrap; border-radius:6px; }
.msg-user { color:var(--accent); font-weight:bold; } .msg-assistant { color:#77dd77; }
#toggle-dark { cursor:pointer; } #drop-area { border:2px dashed var(--border); padding:20px; margin-top:10px; border-radius:10px; text-align:center; transition:border-color .15s ease; }
#drop-area.dragover { border-color:var(--accent); }
.progress { width:100%; background:#222; border-radius:6px; overflow:hidden; border:1px solid var(--border); margin-top:8px; }
.progress > div { height:14px; width:0%; background:linear-gradient(90deg,#3ea6ff,#77dd77); transition:width .2s ease; }
.small { font-size:.9rem; color:var(--muted); }
.chat-area { border:1px solid var(--border); padding:10px; border-radius:8px; max-height:260px; overflow:auto; background:#080808; }
.chat-row { margin-bottom:8px; }
.waiting { opacity:0.9; color:var(--muted); font-style:italic; }
</style>
<script>
document.addEventListener("DOMContentLoaded", () => {
  const saved = localStorage.getItem("theme") || "dark";
  document.documentElement.dataset.theme = saved;
  document.querySelector("#toggle-dark").onclick = () => {
    const current = document.documentElement.dataset.theme;
    const next = current === "dark" ? "light" : "dark";
    document.documentElement.dataset.theme = next;
    localStorage.setItem("theme", next);
  };

  // Drag & drop
  const dropArea = document.getElementById("drop-area");
  const fileInput = document.getElementById("file-input-normal");
  const folderInput = document.getElementById("file-input-folder");
  ['dragenter','dragover'].forEach(e=>dropArea.addEventListener(e,evt=>{evt.preventDefault(); dropArea.classList.add('dragover');}));
  ['dragleave','drop'].forEach(e=>dropArea.addEventListener(e,evt=>{evt.preventDefault(); dropArea.classList.remove('dragover');}));
  dropArea.addEventListener('drop', ev => {
    const dt = ev.dataTransfer;
    if (!dt) return;
    const files = dt.files;
    if (files && files.length>0) {
      const dtNew = new DataTransfer();
      for (let i=0;i<files.length;i++) dtNew.items.add(files[i]);
      fileInput.files = dtNew.files;
      folderInput.files = dtNew.files;
    }
  });

  // Upload XHR progress
  const uploadForm = document.getElementById("upload-form");
  const progressInner = document.getElementById("upload-progress-inner");
  const progressText = document.getElementById("upload-progress-text");

  uploadForm.addEventListener("submit", function(ev){
    ev.preventDefault();
    const formData = new FormData(uploadForm);
    const xhr = new XMLHttpRequest();
    xhr.open("POST", uploadForm.action, true);
    xhr.setRequestHeader("X-Requested-With", "XMLHttpRequest");
    xhr.upload.addEventListener("progress", function(e){
      if (e.lengthComputable) {
        const pct = Math.round((e.loaded / e.total) * 100);
        progressInner.style.width = pct + "%";
        progressText.textContent = pct + "%";
      } else {
        progressText.textContent = "Uploading…";
      }
    });
    xhr.onreadystatechange = function(){
      if (xhr.readyState===4) {
        if (xhr.status>=200 && xhr.status<300) {
          progressInner.style.width = "100%";
          progressText.textContent = "Complete";
          setTimeout(()=>{ window.location.reload(); }, 600);
        } else {
          progressText.textContent = "Error";
          alert("Upload failed: "+xhr.statusText);
        }
      }
    };
    progressInner.style.width = "0%";
    progressText.textContent = "0%";
    xhr.send(formData);
  });

  // CHAT AJAX with "Awaiting model response..." behavior
  const chatForm = document.getElementById("chat-form");
  const chatHistory = document.getElementById("chat-history");
  chatForm.addEventListener("submit", function(ev){
    ev.preventDefault();
    const formData = new FormData(chatForm);
    const userMsg = formData.get("user_prompt") || "";
    // append user message immediately
    const userDiv = document.createElement("div");
    userDiv.className = "chat-row msg-user";
    userDiv.textContent = "You: " + userMsg;
    chatHistory.appendChild(userDiv);
    // append waiting placeholder
    const waitingDiv = document.createElement("div");
    waitingDiv.className = "chat-row waiting";
    waitingDiv.textContent = "AI: ⏳ Awaiting model response...";
    chatHistory.appendChild(waitingDiv);
    chatHistory.scrollTop = chatHistory.scrollHeight;

    // send AJAX
    const xhr = new XMLHttpRequest();
    xhr.open("POST", chatForm.action, true);
    xhr.setRequestHeader("X-Requested-With", "XMLHttpRequest");
    xhr.onreadystatechange = function(){
      if (xhr.readyState===4) {
        if (xhr.status>=200 && xhr.status<300) {
          // replace waiting with assistant response
          waitingDiv.className = "chat-row msg-assistant";
          waitingDiv.textContent = "AI: " + xhr.responseText;
        } else {
          waitingDiv.className = "chat-row msg-assistant";
          waitingDiv.textContent = "AI: [ERROR receiving response]";
        }
        chatHistory.scrollTop = chatHistory.scrollHeight;
      }
    };
    xhr.send(formData);
    // clear input
    chatForm.querySelector('textarea[name="user_prompt"]').value = "";
  });

});
</script>
</head>
<body>
<div class="container">
<button id="toggle-dark">🌓 Toggle Dark Mode</button>
<h2>📂 Ollama Universal Processor + Continuous Chat</h2>

{{if .Error}}
<div style="color:#ff6b6b; padding:10px;">❌ {{.Error}}</div>
{{end}}

<h3>Chat</h3>
<div id="chat-history" class="chat-area">
{{range .History}}
  {{if eq .Role "user"}}<div class="chat-row msg-user">You: {{.Content}}</div>{{end}}
  {{if eq .Role "assistant"}}<div class="chat-row msg-assistant">AI: {{.Content}}</div>{{end}}
{{end}}
</div>

<form id="chat-form" action="/chat" method="post">
<select name="model_select">
  {{range .Models}}<option value="{{.}}">{{.}}</option>{{end}}
</select>
<textarea name="user_prompt" placeholder="Ask something..."></textarea>
<input type="submit" value="💬 Send Message" />
</form>

<hr>

<form id="upload-form" action="/upload" method="post" enctype="multipart/form-data">
<label>Select Files:</label>
<input id="file-input-normal" type="file" name="file_upload" multiple />
<label>Or Select a Folder:</label>
<input id="file-input-folder" type="file" name="file_upload" webkitdirectory directory />

<div id="drop-area">Drag & drop files or folders here</div>

<label>Your Prompt:</label>
<textarea name="user_prompt"></textarea>

<select name="model_select">
  {{range .Models}}<option value="{{.}}">{{.}}</option>{{end}}
</select>

<div class="small">Upload Progress:</div>
<div class="progress"><div id="upload-progress-inner" style="width:0%"></div></div>
<div id="upload-progress-text" class="small">0%</div>

<input type="submit" value="📤 Upload + Process" />
</form>

<hr>
<div class="flex"><a href="/logs" target="_blank">View Live Logs</a><div style="flex:1"></div><div class="small">trace: {{.Trace}}</div></div>
</div>
</body>
</html>
`

const logsHTML = `
<!doctype html>
<html>
<head>
<meta charset="utf-8"/>
<title>Live Logs</title>
<meta name="viewport" content="width=device-width,initial-scale=1"/>
<style>
body { font-family: monospace; background:#111; color:#eee; margin:0; padding:12px; }
#log { height: 90vh; overflow:auto; border:1px solid #333; padding:10px; background:#000; }
.line { white-space:pre; padding:2px 0; }
.err { color:#ff6b6b; } .upload { color:#ffd580; } .ollama { color:#7dd3fc; } .chat { color:#d6b2ff; } .session { color:#9bff9b; }
</style>
</head>
<body>
<h3>Live Logs (auto-scroll)</h3>
<div id="log"></div>
<script>
const logEl = document.getElementById("log");
const evtSource = new EventSource("/logs/stream");
evtSource.onmessage = function(e) {
  const txt = e.data.replace(/\\n/g, "\n");
  const div = document.createElement("div");
  div.className = "line";
  if (txt.indexOf("[ERROR]") !== -1) div.classList.add("err");
  if (txt.indexOf("[UPLOAD]") !== -1) div.classList.add("upload");
  if (txt.indexOf("[OLLAMA]") !== -1) div.classList.add("ollama");
  if (txt.indexOf("[CHAT]") !== -1) div.classList.add("chat");
  if (txt.indexOf("[SESSION]") !== -1) div.classList.add("session");
  div.textContent = txt;
  logEl.appendChild(div);
  logEl.scrollTop = logEl.scrollHeight;
};
evtSource.onerror = function(e) {
  const div = document.createElement("div");
  div.className = "line err";
  div.textContent = "Connection lost. Retrying...";
  logEl.appendChild(div);
};
</script>
</body>
</html>
`
