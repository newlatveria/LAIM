package main

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ollama/ollama/api"
	"code.sajari.com/docconv"
)

/* ---------------------------------------------------------
   SESSION STORAGE
--------------------------------------------------------- */

var sessionHistory = struct {
	sync.Mutex
	H map[string][]api.Message
}{H: make(map[string][]api.Message)}

/* ---------------------------------------------------------
   HELPERS
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

func ProcessSingleFile(file multipart.File, header *multipart.FileHeader) (OllamaInput, error) {
	all, err := io.ReadAll(file)
	if err != nil {
		return OllamaInput{}, err
	}

	mime := http.DetectContentType(all)
	ext := strings.ToLower(filepath.Ext(header.Filename))

	out := OllamaInput{
		Filename: header.Filename,
		MimeType: mime,
	}

	// Images
	if strings.HasPrefix(mime, "image/") {
		out.ImageBytes = [][]byte{all}
		return out, nil
	}

	// Complex docs
	switch ext {
	case ".pdf", ".docx", ".doc", ".xlsx", ".xls", ".pptx", ".ppt", ".odt", ".rtf", ".html":
		res, err := docconv.Convert(strings.NewReader(string(all)), ext, false)
		if err == nil {
			out.TextContent = res.Body
			return out, nil
		}
		log.Printf("docconv failed: %v", err)
	}

	// Raw text fallback
	if isBinary(all) {
		out.TextContent = "[WARNING: binary file]\n" + string(all)
	} else {
		out.TextContent = string(all)
	}

	return out, nil
}

/* ---------------------------------------------------------
   PAGE DATA
--------------------------------------------------------- */

type PageData struct {
	Error         string
	Filenames     []string
	ExtractedText string
	OllamaOutput  string
	Models        []string
	History       []api.Message
}

/* ---------------------------------------------------------
   HTML TEMPLATE RENDERING
--------------------------------------------------------- */

var tmpl = template.Must(template.New("layout").Parse(htmlTemplate))

func renderTemplate(w http.ResponseWriter, data PageData) {
	if len(data.ExtractedText) > 3000 {
		data.ExtractedText = data.ExtractedText[:3000] + "...\n(Truncated)"
	}
	tmpl.Execute(w, data)
}

/* ---------------------------------------------------------
   OLLAMA CALL
--------------------------------------------------------- */

func callOllama(model string, msgs []api.Message) (string, error) {
	client := api.NewClient(&url.URL{Scheme: "http", Host: "localhost:11434"}, http.DefaultClient)

	req := &api.ChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   boolPtr(true),
	}

	var out strings.Builder

	err := client.Chat(context.Background(), req, func(resp api.ChatResponse) error {
		out.WriteString(resp.Message.Content)
		return nil
	})

	return out.String(), err
}

/* ---------------------------------------------------------
   MODEL LISTING
--------------------------------------------------------- */

func listLocalModels() ([]string, error) {
	client := api.NewClient(&url.URL{Scheme: "http", Host: "localhost:11434"}, http.DefaultClient)
	res, err := client.List(context.Background())
	if err != nil {
		return nil, err
	}
	var names []string
	for _, m := range res.Models {
		names = append(names, m.Name)
	}
	return names, nil
}

/* ---------------------------------------------------------
   HANDLERS
--------------------------------------------------------- */

func homeHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(w, r)
	models, _ := listLocalModels()

	sessionHistory.Lock()
	h := sessionHistory.H[sessionID]
	sessionHistory.Unlock()

	renderTemplate(w, PageData{
		Models:  models,
		History: h,
	})
}

func chatHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(w, r)

	msg := r.FormValue("user_prompt")
	model := r.FormValue("model_select")

	if strings.TrimSpace(msg) == "" {
		http.Redirect(w, r, "/", 303)
		return
	}

	sessionHistory.Lock()
	h := sessionHistory.H[sessionID]
	sessionHistory.Unlock()

	h = append(h, api.Message{Role: "user", Content: msg})

	output, err := callOllama(model, h)
	if err != nil {
		renderTemplate(w, PageData{Error: err.Error()})
		return
	}

	h = append(h, api.Message{Role: "assistant", Content: output})

	sessionHistory.Lock()
	sessionHistory.H[sessionID] = h
	sessionHistory.Unlock()

	http.Redirect(w, r, "/", 303)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	sessionID := getSessionID(w, r)

	err := r.ParseMultipartForm(100 << 20)
	if err != nil {
		renderTemplate(w, PageData{Error: err.Error()})
		return
	}

	userPrompt := r.FormValue("user_prompt")
	model := r.FormValue("model_select")

	files := r.MultipartForm.File["file_upload"]
	if len(files) == 0 {
		renderTemplate(w, PageData{Error: "No files uploaded."})
		return
	}

	var text strings.Builder
	var images []api.ImageData
	var names []string

	for _, fh := range files {
		f, _ := fh.Open()
		data, err := ProcessSingleFile(f, fh)
		f.Close()

		if err != nil {
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
	h = append(h, api.Message{
		Role:    "user",
		Content: fullPrompt,
		Images:  images,
	})

	output, err := callOllama(model, h)
	if err != nil {
		renderTemplate(w, PageData{Error: err.Error()})
		return
	}

	h = append(h, api.Message{Role: "assistant", Content: output})

	sessionHistory.Lock()
	sessionHistory.H[sessionID] = h
	sessionHistory.Unlock()

	models, _ := listLocalModels()

	renderTemplate(w, PageData{
		Filenames:     names,
		ExtractedText: text.String(),
		OllamaOutput:  output,
		Models:        models,
		History:       h,
	})
}

/* ---------------------------------------------------------
   MAIN
--------------------------------------------------------- */

func main() {
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.HandleFunc("/chat", chatHandler)

	fmt.Println("Server running at http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

/* ---------------------------------------------------------
   HTML TEMPLATE
--------------------------------------------------------- */

const htmlTemplate = `
<!DOCTYPE html>
<html lang="en" data-theme="dark">
<head>
<meta charset="UTF-8" />
<title>Ollama Universal File Loader + Chat</title>
<meta name="viewport" content="width=device-width, initial-scale=1" />
<style>
/* Dark mode variables */
:root {
  --bg: #111;
  --fg: #eaeaea;
  --accent: #3ea6ff;
  --card: #1e1e1e;
  --border: #333;
}
html[data-theme=light] {
  --bg: #fafafa;
  --fg: #222;
  --accent: #0066cc;
  --card: #fff;
  --border: #ccc;
}

body {
  font-family: system-ui, sans-serif;
  background: var(--bg);
  color: var(--fg);
  margin: 0;
  padding: 20px;
}

.container {
  max-width: 900px;
  margin: auto;
  background: var(--card);
  padding: 20px;
  border-radius: 12px;
  border: 1px solid var(--border);
}

textarea, select, input[type=file] {
  width: 100%;
  padding: 10px;
  border-radius: 6px;
  background: var(--bg);
  color: var(--fg);
  border: 1px solid var(--border);
}

button, input[type=submit] {
  width: 100%;
  padding: 12px;
  background: var(--accent);
  border: none;
  border-radius: 6px;
  cursor: pointer;
  margin-top: 10px;
  color: #000;
  font-size: 1rem;
}

.result {
  background: var(--bg);
  border: 1px solid var(--border);
  padding: 12px;
  white-space: pre-wrap;
  border-radius: 6px;
}

.msg-user { color: var(--accent); font-weight: bold; }
.msg-assistant { color: #77dd77; }

#toggle-dark { cursor: pointer; }
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

{{if .History}}
<h3>Chat History</h3>
<div class="result">
{{range .History}}
  {{if eq .Role "user"}}<div class="msg-user">You: {{.Content}}</div>{{end}}
  {{if eq .Role "assistant"}}<div class="msg-assistant">AI: {{.Content}}</div>{{end}}
{{end}}
</div>
{{end}}

<hr>

<form action="/chat" method="post">
<label>Model:</label>
<select name="model_select">
  {{range .Models}}
    <option value="{{.}}">{{.}}</option>
  {{end}}
</select>

<label>Message:</label>
<textarea name="user_prompt"></textarea>

<input type="submit" value="💬 Send Message" />
</form>

<hr>

<form action="/upload" method="post" enctype="multipart/form-data">

<label>Upload Files:</label>
<!-- Normal file picker -->
<input type="file" name="file_upload" multiple />

<label>Or Upload a Folder:</label>
<!-- Folder picker -->
<input type="file" name="file_upload" webkitdirectory mozdirectory directory />

<!-- Drag & drop area -->
<div id="drop-area" 
     style="border:2px dashed var(--border); padding:20px; margin-top:10px; border-radius:10px; text-align:center;">
  Drag & drop files or folders here
</div>

<script>
// Attach dropped files/folders to the hidden folder input
document.addEventListener("DOMContentLoaded", () => {
  const dropArea = document.getElementById("drop-area");

  dropArea.addEventListener("dragover", e => {
    e.preventDefault();
    dropArea.style.borderColor = "var(--accent)";
  });

  dropArea.addEventListener("dragleave", () => {
    dropArea.style.borderColor = "var(--border)";
  });

  dropArea.addEventListener("drop", e => {
    e.preventDefault();
    dropArea.style.borderColor = "var(--border)";

    const dt = e.dataTransfer;
    const items = dt.items;

    // Convert DataTransferItemList → File[]
    const files = [];
    function traverse(item, path = "") {
      if (item.kind === "file") {
        item.getAsFile().webkitRelativePath = path + item.getAsFile().name;
        files.push(item.getAsFile());
      } else if (item.kind === "directory") {
        const dirReader = item.createReader();
        dirReader.readEntries(entries => {
          for (const entry of entries) {
            traverse(entry, path + item.name + "/");
          }
        });
      }
    }

    for (let i = 0; i < items.length; i++) {
      const entry = items[i].webkitGetAsEntry();
      if (entry) traverse(entry);
    }

    // Build a DataTransfer for the form
    const dtNew = new DataTransfer();
    files.forEach(f => dtNew.items.add(f));

    // Assign dropped items to BOTH inputs
    document.querySelector("input[type=file][multiple]").files = dtNew.files;
    document.querySelector("input[webkitdirectory]").files = dtNew.files;
  });
});
</script>

<label>Your Prompt:</label>
<textarea name="user_prompt"></textarea>

<select name="model_select">
  {{range .Models}}
    <option value="{{.}}">{{.}}</option>
  {{end}}
</select>

<input type="submit" value="📤 Upload + Process" />
</form>

{{if .OllamaOutput}}
<h3>LLM Output</h3>
<div class="result">{{.OllamaOutput}}</div>
{{end}}

{{if .ExtractedText}}
<h3>Extracted Text</h3>
<div class="result">{{.ExtractedText}}</div>
{{end}}

</div>
</body>
</html>
`

