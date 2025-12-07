package main

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/ollama/ollama/api"
	"code.sajari.com/docconv"
)

// --- Helper Functions ---

// boolPtr is required because api.Bool was removed from the Ollama SDK.
func boolPtr(b bool) *bool {
	return &b
}

// --- File Processing Logic ---

// OllamaInput holds the data prepared for the Ollama API request.
type OllamaInput struct {
	TextContent string   // The extracted text content for text-based files/documents
	ImageBytes  [][]byte // Raw bytes of the images (Ollama SDK handles Base64 encoding)
	MimeType    string   // The detected or determined MIME type of the file
}

// ProcessFileForOllama reads an uploaded file, extracts content, and prepares it for Ollama.
func ProcessFileForOllama(file io.Reader, filename string) (OllamaInput, error) {
	// Read file content into a byte slice for initial detection and processing
	allFileBytes, err := io.ReadAll(file)
	if err != nil {
		return OllamaInput{}, fmt.Errorf("failed to read file content: %w", err)
	}

	// Detect the MIME type from the content
	contentType := http.DetectContentType(allFileBytes)
	extension := filepath.Ext(filename)

	input := OllamaInput{
		MimeType: contentType,
	}

	if strings.HasPrefix(contentType, "image/") {
		// Handle Images: Store raw bytes. The SDK will handle Base64 encoding.
		input.ImageBytes = [][]byte{allFileBytes}

	} else if strings.Contains(contentType, "text/") || strings.HasSuffix(filename, ".json") || strings.HasSuffix(filename, ".md") {
		// Handle Plain Text/Code
		input.TextContent = string(allFileBytes)

	} else {
		// Handle Complex Documents (PDF, DOCX, XLSX, etc.) using docconv
		
		// Create a new reader from the byte slice for docconv
		textReader := strings.NewReader(string(allFileBytes))
		
		// FIX 1: docconv.Convert returns a Response struct, not a string.
		response, err := docconv.Convert(textReader, extension, false)
		if err != nil {
			log.Printf("Warning: docconv failed for %s. Attempting raw text fallback. Error: %v", extension, err)
			input.TextContent = string(allFileBytes)
		} else {
			// FIX 1: Access the .Body field to get the text
			input.TextContent = response.Body 
		}
		
		// Update MIME type based on file extension for better identification
		input.MimeType = extension
	}

	return input, nil
}

// --- Ollama API Call Handler ---

// OllamaResponseData holds the data to be passed to the HTML template.
type OllamaResponseData struct {
	Filename      string
	MimeType      string
	ExtractedText string
	Prompt        string
	Model         string
	OllamaOutput  string
	Error         string
}

// uploadHandler processes the file upload and calls the Ollama API.
func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// 1. Parse uploaded file
	file, fileHeader, err := r.FormFile("file_upload")
	if err != nil {
		renderTemplate(w, OllamaResponseData{Error: fmt.Sprintf("Error retrieving file: %v", err)})
		return
	}
	defer file.Close()

	// 2. Process file content for Ollama
	ollamaData, err := ProcessFileForOllama(file, fileHeader.Filename)
	if err != nil {
		renderTemplate(w, OllamaResponseData{Error: fmt.Sprintf("Error processing file for Ollama: %v", err)})
		return
	}
	
	// Prepare base response data for template
	responseData := OllamaResponseData{
		Filename: fileHeader.Filename,
		MimeType: ollamaData.MimeType,
		ExtractedText: ollamaData.TextContent,
	}

	// 3. Prepare Ollama Request
	var prompt string
	var messages []api.Message
	model := "llama3.1:8b" // Default model for text processing

	if len(ollamaData.ImageBytes) > 0 {
		// Requires a multimodal model like 'llava'
		model = "llava" 
		prompt = "What are the main objects and scene details in this image?"
		
		// FIX 2: Convert raw [][]byte to []api.ImageData
		var imageData []api.ImageData
		for _, imgByte := range ollamaData.ImageBytes {
			imageData = append(imageData, api.ImageData(imgByte))
		}

		messages = []api.Message{
			{
				Role:    "user",
				Content: prompt,
				Images:  imageData, 
			},
		}
	} else if ollamaData.TextContent != "" {
		// Text file processing
		prompt = "Please summarize the following document and list three key takeaways."
		fullPrompt := fmt.Sprintf("%s\n\n[DOCUMENT CONTENT START]\n%s\n[DOCUMENT CONTENT END]", prompt, ollamaData.TextContent)
		
		messages = []api.Message{
			{
				Role:    "user",
				Content: fullPrompt,
			},
		}
	} else {
		responseData.Error = "No usable content extracted from file."
		renderTemplate(w, responseData)
		return
	}

	responseData.Prompt = prompt
	responseData.Model = model
	
	// 4. Call Ollama API
	ollamaClient := api.NewClient(&url.URL{Scheme: "http", Host: "localhost:11434"}, http.DefaultClient)

	// Buffer for the streamed response
	var ollamaOutput strings.Builder
	
	req := &api.ChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   boolPtr(true),
	}

	log.Printf("Attempting API call to Ollama model: %s", model)

	err = ollamaClient.Chat(context.Background(), req, func(resp api.ChatResponse) error {
		ollamaOutput.WriteString(resp.Message.Content)
		return nil
	})
	
	if err != nil {
		responseData.OllamaOutput = fmt.Sprintf("Error calling Ollama API (Model: %s): %v. Did you run 'ollama pull %s'?", model, err, model)
	} else {
		responseData.OllamaOutput = ollamaOutput.String()
	}

	// 5. Render results
	renderTemplate(w, responseData)
}

// --- HTTP Server and Routing ---

var tmpl = template.Must(template.New("layout").Parse(htmlTemplate))

func homeHandler(w http.ResponseWriter, r *http.Request) {
	renderTemplate(w, OllamaResponseData{})
}

func renderTemplate(w http.ResponseWriter, data OllamaResponseData) {
	if data.ExtractedText != "" && len(data.ExtractedText) > 1000 {
		data.ExtractedText = data.ExtractedText[:1000] + "...\n(Truncated for display)"
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

// --- HTML Template for the Web Interface ---

const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Ollama File Processor</title>
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background-color: #f4f7f9; }
        .container { max-width: 900px; margin: auto; background: white; padding: 20px; border-radius: 8px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
        h2 { color: #2c3e50; border-bottom: 2px solid #3498db; padding-bottom: 10px; }
        form { margin-bottom: 20px; padding: 15px; border: 1px solid #ccc; border-radius: 5px; background-color: #ecf0f1; }
        input[type="file"] { padding: 10px; border: 1px solid #ccc; border-radius: 4px; }
        input[type="submit"] { background-color: #3498db; color: white; padding: 10px 15px; border: none; border-radius: 4px; cursor: pointer; transition: background-color 0.3s; }
        input[type="submit"]:hover { background-color: #2980b9; }
        .result-box { margin-top: 20px; padding: 15px; border: 1px solid #bdc3c7; border-radius: 5px; background-color: #fff; white-space: pre-wrap; word-wrap: break-word; }
        .error { color: #e74c3c; font-weight: bold; }
        .success { color: #2ecc71; font-weight: bold; }
        .info { background-color: #e8f5e9; border-left: 5px solid #4caf50; padding: 10px; margin-bottom: 15px; }
    </style>
</head>
<body>
    <div class="container">
        <h2>🤖 Ollama File Processor Web Interface</h2>

        {{if .Error}}
            <div class="error result-box">❌ Error: {{.Error}}</div>
        {{end}}

        <form action="/upload" method="post" enctype="multipart/form-data">
            <label for="file_upload">Upload any file (PDF, DOCX, XLSX, Image, TXT):</label><br><br>
            <input type="file" id="file_upload" name="file_upload" required><br><br>
            <input type="submit" value="Process File with Ollama">
        </form>

        {{if .Filename}}
            <div class="info">
                <strong>File:</strong> {{.Filename}} | 
                <strong>Type:</strong> {{.MimeType}} | 
                <strong>Model Used:</strong> {{.Model}}
            </div>

            <h3 class="success">Extracted Text (LLM Input)</h3>
            <div class="result-box">
                {{if .ExtractedText}}
                    <strong>Prompt:</strong> {{.Prompt}}<br><br>
                    {{.ExtractedText}}
                {{else}}
                    (Binary content - image data was passed to {{.Model}}.)
                {{end}}
            </div>
            
            <h3>Ollama Response</h3>
            <div class="result-box">
                {{.OllamaOutput}}
            </div>
        {{end}}
    </div>
</body>
</html>
`