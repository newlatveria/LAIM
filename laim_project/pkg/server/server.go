package server

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	//"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"laim/pkg/files"
	"laim/pkg/logs"
	"laim/pkg/models"
	"laim/pkg/sessions"
	"laim/pkg/templates"

	"github.com/ollama/ollama/api"
)

var (
	tmpl = template.Must(template.New("layout").Parse(templates.HTML))
)

func RegisterRoutes() {
	http.HandleFunc("/", withMiddleware(homeHandler))
	http.HandleFunc("/chat", withMiddleware(chatHandler))
	http.HandleFunc("/upload", withMiddleware(uploadHandler))
	http.HandleFunc("/logs", withMiddleware(logsPageHandler))
	http.HandleFunc("/logs/stream", withMiddleware(logsSSEHandler))
	http.HandleFunc("/models", withMiddleware(modelsHandler))
}

func withMiddleware(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		trace := fmt.Sprintf("%x", time.Now().UnixNano())
		ctx := context.WithValue(r.Context(), "trace", trace)
		r = r.WithContext(ctx)
		start := time.Now()
		log.Printf("[INFO] Incoming %s %s", r.Method, r.URL.Path)

		h(w, r)

		elapsed := time.Since(start)
		log.Printf("[INFO] Request finished in %d ms", elapsed.Milliseconds())
	}
}

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
	log.Printf("[SESSION] Created new session: %s", id)
	return id
}

func renderTemplate(w http.ResponseWriter, r *http.Request, data map[string]interface{}) {
	if s, ok := data["ExtractedText"].(string); ok && len(s) > 3000 {
		data["ExtractedText"] = s[:3000] + "...\n(Truncated)"
	}
	if r != nil {
		data["Trace"] = r.Context().Value("trace")
	}
	_ = tmpl.Execute(w, data)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(w, r)
	log.Printf("[HOME] Session=%s opened homepage", sessionID)

	modelsList, _ := models.ListLocalModels(r)

	sessions.SessionHistory.Lock()
	h := sessions.SessionHistory.H[sessionID]
	sessions.SessionHistory.Unlock()

	renderTemplate(w, r, map[string]interface{}{
		"Models":  modelsList,
		"History": h,
	})
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(w, r)

	msg := r.FormValue("user_prompt")
	model := r.FormValue("model_select")

	log.Printf("[CHAT] Session=%s Model=%s PromptSize=%d", sessionID, model, len(msg))

	if strings.TrimSpace(msg) == "" {
		log.Printf("[CHAT] Empty message — ignored")
		http.Redirect(w, r, "/", 303)
		return
	}

	sessions.SessionHistory.Lock()
	h := sessions.SessionHistory.H[sessionID]
	sessions.SessionHistory.Unlock()

	h = append(h, api.Message{Role: "user", Content: msg})

	output, err := callOllama(model, h)
	if err != nil {
		log.Printf("[ERROR] Ollama call failed: %v", err)
		if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		renderTemplate(w, r, map[string]interface{}{"Error": err.Error()})
		return
	}

	h = append(h, api.Message{Role: "assistant", Content: output})

	sessions.SessionHistory.Lock()
	sessions.SessionHistory.H[sessionID] = h
	sessions.SessionHistory.Unlock()

	log.Printf("[CHAT] AI returned %d chars", len(output))

	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, output)
		return
	}
	http.Redirect(w, r, "/", 303)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(w, r)
	log.Printf("[UPLOAD] Start upload for session=%s", sessionID)

	err := r.ParseMultipartForm(500 << 20) // 500 MB
	if err != nil {
		log.Printf("[ERROR] ParseMultipartForm: %v", err)
		renderTemplate(w, r, map[string]interface{}{"Error": err.Error()})
		return
	}

	userPrompt := r.FormValue("user_prompt")
	model := r.FormValue("model_select")

	filesHeaders := r.MultipartForm.File["file_upload"]
	log.Printf("[UPLOAD] %d files detected", len(filesHeaders))

	if len(filesHeaders) == 0 {
		renderTemplate(w, r, map[string]interface{}{"Error": "No files uploaded."})
		return
	}

	var textBuilder strings.Builder
	var images []api.ImageData
	var names []string

	for _, fh := range filesHeaders {
		log.Printf("[UPLOAD] Handling: %s (%d bytes)", fh.Filename, fh.Size)
		f, _ := fh.Open()
		data, err := files.ProcessSingleFile(f, fh, r)
		f.Close()
		if err != nil {
			log.Printf("[ERROR] Processing file %s: %v", fh.Filename, err)
			continue
		}
		names = append(names, fh.Filename)
		if data.TextContent != "" {
			textBuilder.WriteString("\n--- " + fh.Filename + " ---\n")
			textBuilder.WriteString(data.TextContent + "\n")
		}
		for _, img := range data.ImageBytes {
			images = append(images, api.ImageData(img))
		}
	}

	sessions.SessionHistory.Lock()
	h := sessions.SessionHistory.H[sessionID]
	sessions.SessionHistory.Unlock()

	fullPrompt := fmt.Sprintf("%s\n\n[FILES]\n%s", userPrompt, textBuilder.String())
	log.Printf("[UPLOAD] Sending combined prompt (%d chars, %d images)", len(fullPrompt), len(images))

	h = append(h, api.Message{
		Role:    "user",
		Content: fullPrompt,
		Images:  images,
	})

	output, err := callOllama(model, h)
	if err != nil {
		log.Printf("[ERROR] Ollama upload call failed: %v", err)
		renderTemplate(w, r, map[string]interface{}{"Error": err.Error()})
		return
	}

	h = append(h, api.Message{Role: "assistant", Content: output})

	sessions.SessionHistory.Lock()
	sessions.SessionHistory.H[sessionID] = h
	sessions.SessionHistory.Unlock()

	log.Printf("[UPLOAD] Ollama returned %d chars", len(output))

	modelsList, _ := models.ListLocalModels(r)

	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "OK\nProcessed files: %d\n", len(names))
		return
	}

	renderTemplate(w, r, map[string]interface{}{
		"Filenames":     names,
		"ExtractedText": textBuilder.String(),
		"OllamaOutput":  output,
		"Models":        modelsList,
		"History":       h,
	})
}

func callOllama(model string, msgs []api.Message) (string, error) {
	client := api.NewClient(&url.URL{Scheme: "http", Host: "localhost:11434"}, http.DefaultClient)

	req := &api.ChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   func() *bool { b := true; return &b }(),
	}

	log.Printf("[OLLAMA] Model=%s | Sending prompt (%d messages)", model, len(msgs))

	var out strings.Builder
	err := client.Chat(context.Background(), req, func(resp api.ChatResponse) error {
		out.WriteString(resp.Message.Content)
		return nil
	})
	log.Printf("[OLLAMA] Response size=%d chars", out.Len())
	return out.String(), err
}

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
	logs.TailMu.Lock()
	initial := make([]string, len(logs.TailLines))
	copy(initial, logs.TailLines)
	logs.TailMu.Unlock()

	ch := make(chan string, 100)
	logs.SubscribersMu.Lock()
	logs.Subscribers[ch] = struct{}{}
	logs.SubscribersMu.Unlock()

	defer func() {
		logs.SubscribersMu.Lock()
		delete(logs.Subscribers, ch)
		close(ch)
		logs.SubscribersMu.Unlock()
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
	t := template.Must(template.New("logs").Parse(templates.LogsHTML))
	_ = t.Execute(w, nil)
}

func escapeForSSE(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

func modelsHandler(w http.ResponseWriter, r *http.Request) {
	ml, err := models.ListLocalModels(r)
	if err != nil {
		http.Error(w, "failed listing models", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `[`+"\n")
	for i,m := range ml {
		fmt.Fprintf(w, "%q", m)
		if i < len(ml)-1 {
			io.WriteString(w, ",")
		}
		io.WriteString(w, "\n")
	}
	io.WriteString(w, "]")
}
