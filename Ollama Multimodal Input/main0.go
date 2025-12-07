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
	"unicode/utf8"

	"github.com/ollama/ollama/api"
	"code.sajari.com/docconv"
)

// --- Helper Functions ---

func boolPtr(b bool) *bool {
	return &b
}

// isBinary checks if the content seems to be binary (non-text) data.
// This helps prevent sending garbage characters to the LLM.
func isBinary(content []byte) bool {
	// A simple heuristic: check the first 512 bytes for null characters or invalid UTF-8
	checkLen := len(content)
	if checkLen > 512 {
		checkLen = 512
	}
	
	slice := content[:checkLen]
	if strings.Contains(string(slice), "\x00") {
		return true // Null byte usually means binary
	}
	
	return !utf8.Valid(slice)
}

// --- File Processing Logic ---

type OllamaInput struct {
	TextContent string   
	ImageBytes  [][]byte 
	MimeType    string   
	Filename    string   
}

// ProcessSingleFile reads a single file stream and extracts its content.
func ProcessSingleFile(file multipart.File, header *multipart.FileHeader) (OllamaInput, error) {
	// Read ALL file content
	allFileBytes, err := io.ReadAll(file)
	if err != nil {
		return OllamaInput{}, fmt.Errorf("failed to read file: %w", err)
	}

	contentType := http.DetectContentType(allFileBytes)
	extension := strings.ToLower(filepath.Ext(header.Filename))

	input := OllamaInput{
		MimeType: contentType,
		Filename: header.Filename,
	}

	// 1. IMAGE HANDLING
	if strings.HasPrefix(contentType, "image/") {
		input.ImageBytes = [][]byte{allFileBytes}
		return input, nil
	}

	// 2. COMPLEX DOCUMENT HANDLING (PDF, DOCX, XLSX, PPTX)
	// We only use docconv for specific formats that require parsing.
	switch extension {
	case ".pdf", ".docx", ".doc", ".xlsx", ".xls", ".pptx", ".ppt", ".odt", ".rtf", ".html":
		textReader := strings.NewReader(string(allFileBytes))
		response, err := docconv.Convert(textReader, extension, false)
		if err != nil {
			log.Printf("Warning: docconv failed for %s. Attempting raw text fallback. Error: %v", header.Filename, err)
			// Fall through to text handling below
		} else {
			input.TextContent = response.Body
			return input, nil
		}
	}

	// 3. UNIVERSAL TEXT HANDLING (The "All Files" Logic)
	// Treat everything else as text (Code, Logs, Configs, etc.)
	
	// Safety Check: If it looks like a binary executable (and wasn't handled above), warn but still try.
	if isBinary(allFileBytes) {
		input.TextContent = fmt.Sprintf("[WARNING: File %s appears to be binary data. Content may be garbled.]\n%s", header.Filename, string(allFileBytes))
	} else {
		input.TextContent = string(allFileBytes)
	}

	return input, nil
}

// --- Ollama API Call Handler ---

type OllamaResponseData struct {
	Filenames     []string
	ExtractedText string
	Prompt        string
	Model         string
	OllamaOutput  string
	Error         string
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// 1. Parse Multipart Form (Increased limit to 100MB for larger files)
	err := r.ParseMultipartForm(100 << 20) 
	if err != nil {
		renderTemplate(w, OllamaResponseData{Error: fmt.Sprintf("Error parsing form: %v", err)})
		return
	}

	// Get User Prompt
	userPrompt := r.FormValue("user_prompt")
	if strings.TrimSpace(userPrompt) == "" {
		userPrompt = "Summarize the following content."
	}

	// Get Files
	files := r.MultipartForm.File["file_upload"]
	if len(files) == 0 {
		renderTemplate(w, OllamaResponseData{Error: "No files uploaded."})
		return
	}

	// 2. Process All Files
	var combinedTextBuilder strings.Builder
	var combinedImages []api.ImageData
	var processedFilenames []string
	hasImages := false

	for _, fileHeader := range files {
		file, err := fileHeader.Open()
		if err != nil {
			log.Printf("Error opening file %s: %v", fileHeader.Filename, err)
			continue
		}
		
		data, err := ProcessSingleFile(file, fileHeader)
		file.Close()
		if err != nil {
			log.Printf("Error processing %s: %v", fileHeader.Filename, err)
			continue
		}

		processedFilenames = append(processedFilenames, fileHeader.Filename)

		// Aggregate Text
		if data.TextContent != "" {
			combinedTextBuilder.WriteString(fmt.Sprintf("\n\n--- FILE START: %s ---\n%s\n--- FILE END: %s ---", fileHeader.Filename, data.TextContent, fileHeader.Filename))
		}

		// Aggregate Images
		if len(data.ImageBytes) > 0 {
			hasImages = true
			for _, img := range data.ImageBytes {
				combinedImages = append(combinedImages, api.ImageData(img))
			}
		}
	}

	// 3. Prepare Ollama Request
	model := "llama3.1:8b" 
	if hasImages {
		model = "llava" // Switch to multimodal if images exist
	}

	// Construct the final prompt
	finalPrompt := fmt.Sprintf("%s\n\n[CONTEXT DATA BEGINS]%s\n[CONTEXT DATA ENDS]", userPrompt, combinedTextBuilder.String())

	messages := []api.Message{
		{
			Role:    "user",
			Content: finalPrompt,
			Images:  combinedImages,
		},
	}

	// 4. Call Ollama API
	ollamaClient := api.NewClient(&url.URL{Scheme: "http", Host: "localhost:11434"}, http.DefaultClient)
	
	responseData := OllamaResponseData{
		Filenames:     processedFilenames,
		ExtractedText: combinedTextBuilder.String(),
		Prompt:        userPrompt,
		Model:         model,
	}

	var ollamaOutput strings.Builder
	req := &api.ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   boolPtr(true),
	}

	log.Printf("Sending request to %s with %d files...", model, len(processedFilenames))

	err = ollamaClient.Chat(context.Background(), req, func(resp api.ChatResponse) error {
		ollamaOutput.WriteString(resp.Message.Content)
		return nil
	})

	if err != nil {
		responseData.OllamaOutput = fmt.Sprintf("Error calling Ollama: %v", err)
	} else {
		responseData.OllamaOutput = ollamaOutput.String()
	}

	renderTemplate(w, responseData)
}

// --- HTTP Server and Routing ---

var tmpl = template.Must(template.New("layout").Parse(htmlTemplate))

func homeHandler(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, OllamaResponseData{})
}

func renderTemplate(w http.ResponseWriter, data OllamaResponseData) {
	// Truncate display text if it's too huge for the HTML page
	if len(data.ExtractedText) > 3000 {
		data.ExtractedText = data.ExtractedText[:3000] + "...\n(Truncated for display - full content sent to LLM)"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "Failed to render template", http.StatusInternalServerError)
	}
}

func main() {
	http.HandleFunc("/", homeHandler)
	http.HandleFunc("/upload", uploadHandler)

	fmt.Println("Server starting on http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// --- HTML Template ---

const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Ollama Universal File Processor</title>
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        body { font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif; margin: 0; background-color: #f0f2f5; color: #333; }
        .container { max-width: 900px; margin: 40px auto; background: white; padding: 30px; border-radius: 12px; box-shadow: 0 4px 15px rgba(0,0,0,0.1); }
        h2 { color: #2c3e50; border-bottom: 2px solid #3498db; padding-bottom: 15px; margin-top: 0; }
        
        .form-group { margin-bottom: 20px; }
        label { display: block; margin-bottom: 8px; font-weight: bold; color: #555; }
        
        input[type="file"] { width: 100%; padding: 10px; border: 2px dashed #bdc3c7; border-radius: 6px; background: #fafafa; }
        textarea { width: 100%; height: 100px; padding: 12px; border: 1px solid #ccc; border-radius: 6px; font-family: inherit; resize: vertical; box-sizing: border-box; }
        
        input[type="submit"] { background-color: #3498db; color: white; padding: 12px 24px; border: none; border-radius: 6px; cursor: pointer; font-size: 16px; transition: background 0.3s; width: 100%; }
        input[type="submit"]:hover { background-color: #2980b9; }

        .result-section { margin-top: 30px; border-top: 1px solid #eee; padding-top: 20px; }
        .result-box { background: #f8f9fa; border: 1px solid #e9ecef; padding: 20px; border-radius: 8px; white-space: pre-wrap; line-height: 1.6; }
        .file-list { background: #e8f5e9; padding: 10px; border-radius: 4px; color: #2e7d32; font-size: 0.9em; margin-bottom: 15px; }
        .error { color: #d32f2f; background: #ffebee; padding: 15px; border-radius: 6px; border: 1px solid #ffcdd2; }
    </style>
</head>
<body>
    <div class="container">
        <h2>📂 Ollama Universal File Loader</h2>

        {{if .Error}}
            <div class="error">❌ {{.Error}}</div>
        {{end}}

        <form action="/upload" method="post" enctype="multipart/form-data">
            
            <div class="form-group">
                <label for="user_prompt">1. Prompt / Instructions:</label>
                <textarea id="user_prompt" name="user_prompt" placeholder="e.g. 'Read these code files and find the bug,' or 'Summarize these logs.'"></textarea>
            </div>

            <div class="form-group">
                <label for="model_select">2. Select Model:</label>
                <select id="model_select" name="model_select">
                    <option value="llama3.1:8b" selected>llama3.1:8b (Text Default)</option>
                    <option value="llava">llava (Multimodal)</option>
                    <option value="mistral">mistral</option>
                    <option value="gemma">gemma</option>
                    <option value="phi3">phi3</option>
                </select>
            </div>

            <div class="form-group" style="padding:15px; border: 1px solid #ccc; border-radius: 6px;">
                <label style="margin-bottom: 15px;">3. Select Content:</label>
                
                <div style="margin-bottom: 10px;">
                    <label for="file_upload_files" style="font-weight: normal; margin-bottom: 5px;">A. Upload **Individual Files** (Select multiple files):</label>
                    <input type="file" id="file_upload_files" name="file_upload" multiple style="border: 1px solid #ccc; background: #fff;">
                </div>
                
                <div>
                    <label for="file_upload_folder" style="font-weight: normal; margin-bottom: 5px;">B. Upload an **Entire Folder**:</label>
                    <input type="file" id="file_upload_folder" name="file_upload" webkitdirectory directory multiple style="border: 1px solid #ccc; background: #fff;">
                </div>
                <small style="color: #777; display: block; margin-top: 10px;">You can use either or both methods (A and B).</small>
            </div>
            <input type="submit" value="🚀 Process Everything">
        </form>

        {{if .Filenames}}
            <div class="result-section">
                <div class="file-list">
                    <strong>📂 Uploaded {{len .Filenames}} files:</strong> 
                    {{range .Filenames}} <span style="display:inline-block; background:#fff; border:1px solid #ccc; padding:2px 5px; margin:2px; border-radius:3px;">{{.}}</span> {{end}}
                </div>
                
                <h3>🤖 {{.Model}} Response</h3>
                <div class="result-box">{{.OllamaOutput}}</div>

                <details>
                    <summary style="cursor:pointer; color:#777; margin-top:10px;">View Raw Extracted Text</summary>
                    <div class="result-box" style="font-size: 0.8em; color: #555;">{{.ExtractedText}}</div>
                </details>
            </div>
        {{end}}
    </div>
</body>
</html>
`