package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sort"
	"time"
)

// --- API Constants ---

const ollamaBaseURL = "http://localhost:11434"
const ollamaTagsAPI = ollamaBaseURL + "/api/tags"

const huggingFaceBaseURL = "https://huggingface.co"
const huggingFaceModelsAPI = huggingFaceBaseURL + "/api/models"

// --- Ollama API Structures ---

// OllamaTagsResponse structure for /api/tags
type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

// OllamaModel structure for individual models from /api/tags
type OllamaModel struct {
	Name string `json:"name"`
}

// --- Hugging Face API Structures ---

// HuggingFaceModel is the structure for a single item in the /api/models search results
type HuggingFaceModel struct {
	ModelId     string   `json:"modelId"`
	PipelineTag string   `json:"pipeline_tag"` // e.g., "text-generation", "image-classification"
	Tags        []string `json:"tags"`        // Detailed tags like "gemma", "2b", "text", "pytorch"
}

// --- Recommender Data Structures ---

// HardwareSpecs defines the minimum required hardware for a model.
type HardwareSpecs struct {
	MinVRAM_GB int `json:"min_vram_gb"`
	MinRAM_GB  int `json:"min_ram_gb"`
}

// RecommendedModel includes model info, tasks, and its hardware requirements.
type RecommendedModel struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Tasks       []string      `json:"tasks"`
	HardwareReq HardwareSpecs `json:"hardware_req"`
	Score       int           `json:"score"`
}

// ModelDatabase holds all known models and their properties (dynamically populated at startup).
var ModelDatabase = make(map[string]RecommendedModel)

// StaticMetadata holds the non-Ollama-provided data (tasks, hardware) indexed by model name.
var StaticMetadata = map[string]RecommendedModel{
	"tinyllama": {
		Name: "tinyllama",
		Description: "A compact language model, greapackage main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sort"
	"time"
)

// --- API Constants ---

const ollamaBaseURL = "http://localhost:11434"
const ollamaTagsAPI = ollamaBaseURL + "/api/tags"

const huggingFaceBaseURL = "https://huggingface.co"
const huggingFaceModelsAPI = huggingFaceBaseURL + "/api/models"

// --- Ollama API Structures ---

// OllamaTagsResponse structure for /api/tags
type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

// OllamaModel structure for individual models from /api/tags
type OllamaModel struct {
	Name string `json:"name"`
}

// --- Hugging Face API Structures ---

// HuggingFaceModel is the structure for a single item in the /api/models search results
type HuggingFaceModel struct {
	ModelId     string   `json:"modelId"`
	PipelineTag string   `json:"pipeline_tag"` // e.g., "text-generation", "image-classification"
	Tags        []string `json:"tags"`        // Detailed tags like "gemma", "2b", "text", "pytorch"
}

// --- Recommender Data Structures ---

// HardwareSpecs defines the minimum required hardware for a model.
type HardwareSpecs struct {
	MinVRAM_GB int `json:"min_vram_gb"`
	MinRAM_GB  int `json:"min_ram_gb"`
}

// RecommendedModel includes model info, tasks, and its hardware requirements.
type RecommendedModel struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Tasks       []string      `json:"tasks"`
	HardwareReq HardwareSpecs `json:"hardware_req"`
	Score       int           `json:"score"`
}

// ModelDatabase holds all known models and their properties (dynamically populated at startup).
var ModelDatabase = make(map[string]RecommendedModel)

// StaticMetadata holds the non-Ollama-provided data (tasks, hardware) indexed by model name.
var StaticMetadata = map[string]RecommendedModel{
	"tinyllama": {
		Name: "tinyllama",
		Description: "A compact language model, great for resource-constrained environments or quick experiments. Ideal for simple tasks.",
		Tasks:       []string{"chat", "summarization", "experiment"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 2, MinRAM_GB: 4},
		Score:       5,
	},
	"mistral": {
		Name: "mistral",
		Description: "A small, yet powerful, language model from Mistral AI, optimized for performance. Excellent general purpose model.",
		Tasks:       []string{"chat", "generate", "code", "general"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 6, MinRAM_GB: 8},
		Score:       8,
	},
	"llama2:7b-chat": {
		Name: "llama2:7b-chat",
		Description: "The 7-billion parameter chat variant of Meta's Llama 2. A strong baseline model for conversational AI.",
		Tasks:       []string{"chat", "generate", "general"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 8, MinRAM_GB: 16},
		Score:       7,
	},
	"codellama:7b-code": {
		Name: "codellama:7b-code",
		Description: "A model from Meta specifically fine-tuned for code generation and understanding.",
		Tasks:       []string{"code", "generate", "programming"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 8, MinRAM_GB: 16},
		Score:       9,
	},
	"gemma:2b": {
		Name: "gemma:2b",
		Description: "A lightweight, high-quality open model from Google. Great for efficiency.",
		Tasks:       []string{"chat", "summarization", "generate", "experiment"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 3, MinRAM_GB: 6},
		Score:       6,
	},
	"llama2:13b": {
		Name: "llama2:13b",
		Description: "The 13-billion parameter version of Llama 2. Requires substantial resources for good performance.",
		Tasks:       []string{"chat", "generate", "advanced", "general"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 12, MinRAM_GB: 32},
		Score:       10,
	},
	"default-placeholder": {
		Description: "Assigned generic tasks and default hardware requirements (8 GB VRAM / 16 GB RAM).",
		Tasks:       []string{"chat", "generate", "general"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 8, MinRAM_GB: 16},
		Score:       6,
	},
}

// --- Hugging Face Enrichment Logic (Omitted for brevity, assumed unchanged) ---

// enrichModelFromHuggingFace attempts to fetch metadata for an unknown model from Hugging Face.
// Returns an updated description and tasks list.
func enrichModelFromHuggingFace(ollamaModelName string, placeholder RecommendedModel) (string, []string) {
	// 1. Clean the model name for a better search (e.g., 'deepseek-r1:14b' -> 'deepseek-r1')
	parts := strings.Split(ollamaModelName, ":")
	searchQuery := parts[0]

	// 2. Build the search URL. We search for the base model name and limit to 1 result.
	searchURL := fmt.Sprintf("%s?search=%s&limit=1", huggingFaceModelsAPI, searchQuery)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(searchURL)
	if err != nil {
		log.Printf("HF search failed for %s: %v", ollamaModelName, err)
		return fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", ollamaModelName, placeholder.Description), placeholder.Tasks
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("HF search API returned non-200 status %d for %s", resp.StatusCode, ollamaModelName)
		return fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", ollamaModelName, placeholder.Description), placeholder.Tasks
	}

	var results []HuggingFaceModel
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		log.Printf("Failed to decode HF response for %s: %v", ollamaModelName, err)
		return fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", ollamaModelName, placeholder.Description), placeholder.Tasks
	}

	if len(results) == 0 {
		log.Printf("HF search found no results for %s", searchQuery)
		return fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", ollamaModelName, placeholder.Description), placeholder.Tasks
	}

	hfModel := results[0]

	// 3. Extract PipelineTag and Tags to form a better description and task list
	newTasks := placeholder.Tasks
	
	// Use the pipeline tag if available, as it's the most reliable task indicator
	if hfModel.PipelineTag != "" {
		newTasks = []string{strings.Replace(hfModel.PipelineTag, "-", " ", -1)} // "text-generation" -> "text generation"
	} else if len(hfModel.Tags) > 0 {
		// Fallback to tags if no pipeline tag, filtering for common relevant tags
		newTasks = []string{}
		for _, tag := range hfModel.Tags {
			if strings.Contains(tag, "llama") || strings.Contains(tag, "mistral") || strings.Contains(tag, "code") || strings.Contains(tag, "chat") {
				newTasks = append(newTasks, tag)
			}
		}
		if len(newTasks) == 0 {
			newTasks = placeholder.Tasks
		}
	}

	// 4. Construct the enriched description
	
	// Create a clean, comma-separated list of tasks for the description
	taskString := strings.Join(newTasks, ", ")
	
	hfDescription := fmt.Sprintf(
		"Model '%s' is installed on Ollama. Found potential match on Hugging Face as '%s'. Primary tasks identified: %s. Hardware estimates remain at default (8 GB VRAM / 16 GB RAM).",
		ollamaModelName, hfModel.ModelId, taskString)
		
	log.Printf("   -> HF Enrichment successful for %s. Pipeline Tag: %s, Tasks: %v", ollamaModelName, hfModel.PipelineTag, newTasks)
	return hfDescription, newTasks
}

// --- Ollama Fetch and Merge Logic (Omitted for brevity, assumed unchanged) ---

// fetchAndMergeModels fetches the list of available models from Ollama and merges it with static and Hugging Face metadata.
func fetchAndMergeModels() {
	log.Println("Attempting to connect to Ollama to fetch available models...")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ollamaTagsAPI)
	if err != nil {
		log.Printf("⚠️ WARNING: Could not connect to Ollama at %s. Using hardcoded list only. Error: %v", ollamaTagsAPI, err)
		for _, model := range StaticMetadata {
			if model.Name != "default-placeholder" {
				ModelDatabase[model.Name] = model
			}
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("⚠️ WARNING: Ollama tags API returned non-200 status: %d. Using hardcoded list only.", resp.StatusCode)
		for _, model := range StaticMetadata {
			if model.Name != "default-placeholder" {
				ModelDatabase[model.Name] = model
			}
		}
		return
	}

	var tagsResponse OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
		log.Printf("⚠️ WARNING: Failed to decode Ollama response. Using hardcoded list only. Error: %v", err)
		for _, model := range StaticMetadata {
			if model.Name != "default-placeholder" {
				ModelDatabase[model.Name] = model
			}
		}
		return
	}
	
	// --- Merge Logic ---
	log.Printf("✅ Successfully fetched %d models from local Ollama instance. Merging metadata...", len(tagsResponse.Models))

	// Get the default/placeholder metadata
	placeholder := StaticMetadata["default-placeholder"]

	for _, ollamaModel := range tagsResponse.Models {
		modelName := strings.TrimSuffix(ollamaModel.Name, ":latest") // Cleanup tag if present

		if static, ok := StaticMetadata[modelName]; ok {
			// Case 1: Model found in static metadata (e.g., 'llama2:7b-chat')
			ModelDatabase[modelName] = static
			log.Printf("   -> Added (Known): %s", modelName)
		} else {
			// Case 2: Model found on Ollama but not in static metadata (e.g., 'phi3:mini')
			
			// New Logic: Try to enrich metadata from Hugging Face
			enrichedDescription, enrichedTasks := enrichModelFromHuggingFace(modelName, placeholder)
			
			// Fallback description for when HF enrichment failed
			if strings.Contains(enrichedDescription, "metadata is missing") {
			    enrichedDescription = fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", modelName, placeholder.Description)
			}


			newModel := RecommendedModel{
				Name:        modelName,
				Description: enrichedDescription,
				Tasks:       enrichedTasks,
				HardwareReq: placeholder.HardwareReq,
				Score:       placeholder.Score,
			}
			ModelDatabase[modelName] = newModel
			log.Printf("   -> Added (Unknown/Placeholder, Enriched): %s", modelName)
		}
	}
	
	log.Printf("⭐ Final Model Database size: %d", len(ModelDatabase))
}

// --- Utility: Extract Unique Tasks ---

// getUniqueTasks compiles a sorted list of all unique tasks from the current model database.
func getUniqueTasks() []string {
	taskSet := make(map[string]bool)
	// Iterate over the map values (RecommendedModel structs)
	for _, model := range ModelDatabase {
		for _, task := range model.Tasks {
			taskSet[task] = true
		}
	}
	
	var tasks []string
	for task := range taskSet {
		tasks = append(tasks, task)
	}
	
	sort.Strings(tasks)
	return tasks
}

// TemplateData holds data needed to render the HTML template.
type TemplateData struct {
    UniqueTasks []string
}

// --- Hardware/Recommendation Logic ---

type CurrentHardwareSpecs struct {
	VRAM_GB int
	RAM_GB  int
}

func recommendModels(currentHardware CurrentHardwareSpecs, task string) []RecommendedModel {
	var results []RecommendedModel
	task = strings.ToLower(task)

	for _, model := range ModelDatabase {
		if currentHardware.VRAM_GB < model.HardwareReq.MinVRAM_GB || currentHardware.RAM_GB < model.HardwareReq.MinRAM_GB {
			continue
		}

		if task != "" {
			isSuitable := false
			for _, t := range model.Tasks {
				if strings.Contains(t, task) {
					isSuitable = true
					break
				}
			}
			if !isSuitable {
				continue
			}
		}
		results = append(results, model)
	}
	return results
}

// --- Logging Middleware ---

// loggingMiddleware wraps an http.Handler to log details about the request and its processing time.
func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// 1. Log request details BEFORE the handler runs
		log.Printf("➡️ START: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		// 2. Call the next handler in the chain
		next.ServeHTTP(w, r)

		// 3. Log request details AFTER the handler runs
		log.Printf("⬅️ END: %s %s processed in %v", r.Method, r.URL.Path, time.Since(start))
	}
}


// --- API Handler ---

func handleRecommendations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	vramStr := r.URL.Query().Get("vram")
	ramStr := r.URL.Query().Get("ram")
	task := r.URL.Query().Get("task")

	vram, err := strconv.Atoi(vramStr)
	if err != nil {
		vram = 8
	}
	ram, err := strconv.Atoi(ramStr)
	if err != nil {
		ram = 16
	}
    
    currentHardware := CurrentHardwareSpecs{VRAM_GB: vram, RAM_GB: ram}

	recommendations := recommendModels(currentHardware, task)

	responsePayload := map[string]interface{}{
		"current_hardware": map[string]string{
			"vram": fmt.Sprintf("%d GB (Manual Input)", currentHardware.VRAM_GB),
			"ram":  fmt.Sprintf("%d GB (Manual Input)", currentHardware.RAM_GB),
		},
		"recommendations": recommendations,
	}

	if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// --- Web UI Handler (Omitted for brevity, assumed unchanged) ---

var webTemplate = template.Must(template.New("ui").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>LLM Recommender Dev UI</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background-color: #f4f4f4; color: #333; }
        h1, h2 { color: #0056b3; }
        .container { background-color: #fff; padding: 20px; border-radius: 8px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
        .hardware-info { background-color: #e9ecef; padding: 15px; border-radius: 4px; margin-bottom: 20px; display: flex; flex-direction: column; gap: 10px; }
        .input-group { display: flex; align-items: center; gap: 10px; }
        .input-group label { font-weight: bold; }
        .input-group input, .input-group select { padding: 5px; border: 1px solid #ccc; border-radius: 3px; }
        .input-group input[type="number"] { width: 80px; text-align: right; }
        .input-group select { width: 150px; }
        button { padding: 8px 15px; background-color: #28a745; color: white; border: none; border-radius: 4px; cursor: pointer; }
        table { width: 100%; border-collapse: collapse; margin-top: 15px; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #007bff; color: white; }
    </style>
</head>
<body>

<div class="container">
    <h1>LLM Recommender Dev Interface</h1>

    <div class="hardware-info">
        <h2>Simulated Hardware Profile & Filters</h2>
        <div class="input-group">
            <label for="vram">VRAM (GPU Memory):</label>
            <input type="number" id="vram" value="8" min="1">
            <label for="ram">RAM (System Memory):</label>
            <input type="number" id="ram" value="16" min="1">
            
            <label for="task">Filter by Task:</label>
            <select id="task">
                <option value="">-- All Tasks --</option>
                {{range .UniqueTasks}}
                    <option value="{{.}}">{{.}}</option>
                {{end}}
            </select>
            
            <button type="button" onclick="fetchRecommendations()">Get Recommendations</button>
        </div>
        <p style="font-size:0.9em; margin-top: 10px;" id="status-message">
            Enter hardware details above and click 'Get Recommendations'. Defaults are 8 GB VRAM / 16 GB RAM.
        </p>
    </div>

    <h2>Recommended Models</h2>
    <table id="recommendations-table">
        <thead>
            <tr>
                <th>Model</th>
                <th>Description</th>
                <th>Tasks</th>
                <th>Min VRAM (GB)</th>
                <th>Min RAM (GB)</th>
            </tr>
        </thead>
        <tbody>
            </tbody>
    </table>
</div>

<script>
    const API_URL = "/api/v1/recommendations";

    async function fetchRecommendations() {
        const vramInput = document.getElementById('vram').value;
        const ramInput = document.getElementById('ram').value;
        const taskInput = document.getElementById('task').value; // Changed to select element

        const statusMessage = document.getElementById('status-message');

        const params = new URLSearchParams();
        params.append('vram', vramInput || '8');
        params.append('ram', ramInput || '16');
        if (taskInput) {
            params.append('task', taskInput);
        }

        const url = API_URL + "?" + params.toString();
        statusMessage.innerHTML = 'Fetching recommendations...';

        try {
            const response = await fetch(url);
            const data = await response.json();
            
            // Update status message with actual query data
            const hw = data.current_hardware;
            const taskText = taskInput ? ' for task "' + taskInput + '"' : '';
            statusMessage.innerHTML = 'Recommendations filtered using VRAM: <strong>' + hw.vram + '</strong>, RAM: <strong>' + hw.ram + '</strong>' + taskText + '.';

            // Display Recommendations
            const tbody = document.getElementById('recommendations-table').querySelector('tbody');
            tbody.innerHTML = ''; // Clear previous results

            if (data.recommendations && data.recommendations.length > 0) {
                data.recommendations.forEach(model => {
                    const row = tbody.insertRow();
                    row.insertCell().textContent = model.name;
                    row.insertCell().textContent = model.description;
                    row.insertCell().textContent = model.tasks.join(', ');
                    row.insertCell().textContent = model.hardware_req.min_vram_gb;
                    row.insertCell().textContent = model.hardware_req.min_ram_gb;
                });
            } else {
                const row = tbody.insertCell();
                row.colSpan = 5;
                row.textContent = "No recommended models found for the given criteria.";
            }

        } catch (error) {
            console.error("Error fetching recommendations:", error);
            statusMessage.innerHTML = 'Error fetching data from the API service. Check console for details.';
        }
    }

    // Load initial data on page load
    window.onload = fetchRecommendations;
</script>

</body>
</html>
`))

func handleWebUI(w http.ResponseWriter, r *http.Request) {
	// 1. Get the list of tasks from the database (now includes tasks from dynamically loaded models)
	data := TemplateData{
		UniqueTasks: getUniqueTasks(),
	}

	// 2. Execute the template with the task list
	if err := webTemplate.Execute(w, data); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// --- Main Server Logic ---

func main() {
	// Initialize ModelDatabase by fetching models and merging metadata
	fetchAndMergeModels()

	port := os.Getenv("RECOMMENDER_PORT")
	if port == "" {
		port = "8081"
	}

	// Handler registrations - Now wrapped with loggingMiddleware
	http.HandleFunc("/", loggingMiddleware(handleWebUI))
	http.HandleFunc("/api/v1/recommendations", loggingMiddleware(handleRecommendations))

	log.Printf("--- LLM Recommender Service Starting ---")
	log.Printf("Web UI available at: http://localhost:%s/", port)
	log.Printf("API Endpoint: http://localhost:%s/api/v1/recommendations", port)

	log.Fatal(http.ListenAndServe(":"+port, nil))
}t for resource-constrained environments or quick experiments. Ideal for simple tasks.",
		Tasks:       []string{"chat", "summarization", "experiment"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 2, MinRAM_GB: 4},
		Score:       5,
	},
	"mistral": {
		Name: "mistral",
		Description: "A small, yet powerful, language model from Mistral AI, optimized for performance. Excellent general purpose model.",
		Tasks:       []string{"chat", "generate", "code", "general"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 6, MinRAM_GB: 8},
		Score:       8,
	},
	"llama2:7b-chat": {
		Name: "llama2:7b-chat",
		Description: "The 7-billion parameter chat variant of Meta's Llama 2. A strong baseline model for conversational AI.",
		Tasks:       []string{"chat", "generate", "general"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 8, MinRAM_GB: 16},
		Score:       7,
	},
	"codellama:7b-code": {
		Name: "codellama:7b-code",
		Description: "A model from Meta specifically fine-tuned for code generation and understanding.",
		Tasks:       []string{"code", "generate", "programming"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 8, MinRAM_GB: 16},
		Score:       9,
	},
	"gemma:2b": {
		Name: "gemma:2b",
		Description: "A lightweight, high-quality open model from Google. Great for efficiency.",
		Tasks:       []string{"chat", "summarization", "generate", "experiment"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 3, MinRAM_GB: 6},
		Score:       6,
	},
	"llama2:13b": {
		Name: "llama2:13b",
		Description: "The 13-billion parameter version of Llama 2. Requires substantial resources for good performance.",
		Tasks:       []string{"chat", "generate", "advanced", "general"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 12, MinRAM_GB: 32},
		Score:       10,
	},
	"default-placeholder": {
		Description: "Assigned generic tasks and default hardware requirements (8 GB VRAM / 16 GB RAM).",
		Tasks:       []string{"chat", "generate", "general"},
		HardwareReq: HardwareSpecs{MinVRAM_GB: 8, MinRAM_GB: 16},
		Score:       6,
	},
}

// --- Hugging Face Enrichment Logic (Omitted for brevity, assumed unchanged) ---

// enrichModelFromHuggingFace attempts to fetch metadata for an unknown model from Hugging Face.
// Returns an updated description and tasks list.
func enrichModelFromHuggingFace(ollamaModelName string, placeholder RecommendedModel) (string, []string) {
	// 1. Clean the model name for a better search (e.g., 'deepseek-r1:14b' -> 'deepseek-r1')
	parts := strings.Split(ollamaModelName, ":")
	searchQuery := parts[0]

	// 2. Build the search URL. We search for the base model name and limit to 1 result.
	searchURL := fmt.Sprintf("%s?search=%s&limit=1", huggingFaceModelsAPI, searchQuery)

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(searchURL)
	if err != nil {
		log.Printf("HF search failed for %s: %v", ollamaModelName, err)
		return fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", ollamaModelName, placeholder.Description), placeholder.Tasks
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("HF search API returned non-200 status %d for %s", resp.StatusCode, ollamaModelName)
		return fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", ollamaModelName, placeholder.Description), placeholder.Tasks
	}

	var results []HuggingFaceModel
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		log.Printf("Failed to decode HF response for %s: %v", ollamaModelName, err)
		return fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", ollamaModelName, placeholder.Description), placeholder.Tasks
	}

	if len(results) == 0 {
		log.Printf("HF search found no results for %s", searchQuery)
		return fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", ollamaModelName, placeholder.Description), placeholder.Tasks
	}

	hfModel := results[0]

	// 3. Extract PipelineTag and Tags to form a better description and task list
	newTasks := placeholder.Tasks
	
	// Use the pipeline tag if available, as it's the most reliable task indicator
	if hfModel.PipelineTag != "" {
		newTasks = []string{strings.Replace(hfModel.PipelineTag, "-", " ", -1)} // "text-generation" -> "text generation"
	} else if len(hfModel.Tags) > 0 {
		// Fallback to tags if no pipeline tag, filtering for common relevant tags
		newTasks = []string{}
		for _, tag := range hfModel.Tags {
			if strings.Contains(tag, "llama") || strings.Contains(tag, "mistral") || strings.Contains(tag, "code") || strings.Contains(tag, "chat") {
				newTasks = append(newTasks, tag)
			}
		}
		if len(newTasks) == 0 {
			newTasks = placeholder.Tasks
		}
	}

	// 4. Construct the enriched description
	
	// Create a clean, comma-separated list of tasks for the description
	taskString := strings.Join(newTasks, ", ")
	
	hfDescription := fmt.Sprintf(
		"Model '%s' is installed on Ollama. Found potential match on Hugging Face as '%s'. Primary tasks identified: %s. Hardware estimates remain at default (8 GB VRAM / 16 GB RAM).",
		ollamaModelName, hfModel.ModelId, taskString)
		
	log.Printf("   -> HF Enrichment successful for %s. Pipeline Tag: %s, Tasks: %v", ollamaModelName, hfModel.PipelineTag, newTasks)
	return hfDescription, newTasks
}

// --- Ollama Fetch and Merge Logic (Omitted for brevity, assumed unchanged) ---

// fetchAndMergeModels fetches the list of available models from Ollama and merges it with static and Hugging Face metadata.
func fetchAndMergeModels() {
	log.Println("Attempting to connect to Ollama to fetch available models...")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(ollamaTagsAPI)
	if err != nil {
		log.Printf("⚠️ WARNING: Could not connect to Ollama at %s. Using hardcoded list only. Error: %v", ollamaTagsAPI, err)
		for _, model := range StaticMetadata {
			if model.Name != "default-placeholder" {
				ModelDatabase[model.Name] = model
			}
		}
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("⚠️ WARNING: Ollama tags API returned non-200 status: %d. Using hardcoded list only.", resp.StatusCode)
		for _, model := range StaticMetadata {
			if model.Name != "default-placeholder" {
				ModelDatabase[model.Name] = model
			}
		}
		return
	}

	var tagsResponse OllamaTagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResponse); err != nil {
		log.Printf("⚠️ WARNING: Failed to decode Ollama response. Using hardcoded list only. Error: %v", err)
		for _, model := range StaticMetadata {
			if model.Name != "default-placeholder" {
				ModelDatabase[model.Name] = model
			}
		}
		return
	}
	
	// --- Merge Logic ---
	log.Printf("✅ Successfully fetched %d models from local Ollama instance. Merging metadata...", len(tagsResponse.Models))

	// Get the default/placeholder metadata
	placeholder := StaticMetadata["default-placeholder"]

	for _, ollamaModel := range tagsResponse.Models {
		modelName := strings.TrimSuffix(ollamaModel.Name, ":latest") // Cleanup tag if present

		if static, ok := StaticMetadata[modelName]; ok {
			// Case 1: Model found in static metadata (e.g., 'llama2:7b-chat')
			ModelDatabase[modelName] = static
			log.Printf("   -> Added (Known): %s", modelName)
		} else {
			// Case 2: Model found on Ollama but not in static metadata (e.g., 'phi3:mini')
			
			// New Logic: Try to enrich metadata from Hugging Face
			enrichedDescription, enrichedTasks := enrichModelFromHuggingFace(modelName, placeholder)
			
			// Fallback description for when HF enrichment failed
			if strings.Contains(enrichedDescription, "metadata is missing") {
			    enrichedDescription = fmt.Sprintf("Model '%s' is installed on Ollama, but specific metadata is missing. %s", modelName, placeholder.Description)
			}


			newModel := RecommendedModel{
				Name:        modelName,
				Description: enrichedDescription,
				Tasks:       enrichedTasks,
				HardwareReq: placeholder.HardwareReq,
				Score:       placeholder.Score,
			}
			ModelDatabase[modelName] = newModel
			log.Printf("   -> Added (Unknown/Placeholder, Enriched): %s", modelName)
		}
	}
	
	log.Printf("⭐ Final Model Database size: %d", len(ModelDatabase))
}

// --- Utility: Extract Unique Tasks ---

// getUniqueTasks compiles a sorted list of all unique tasks from the current model database.
func getUniqueTasks() []string {
	taskSet := make(map[string]bool)
	// Iterate over the map values (RecommendedModel structs)
	for _, model := range ModelDatabase {
		for _, task := range model.Tasks {
			taskSet[task] = true
		}
	}
	
	var tasks []string
	for task := range taskSet {
		tasks = append(tasks, task)
	}
	
	sort.Strings(tasks)
	return tasks
}

// TemplateData holds data needed to render the HTML template.
type TemplateData struct {
    UniqueTasks []string
}

// --- Hardware/Recommendation Logic ---

type CurrentHardwareSpecs struct {
	VRAM_GB int
	RAM_GB  int
}

func recommendModels(currentHardware CurrentHardwareSpecs, task string) []RecommendedModel {
	var results []RecommendedModel
	task = strings.ToLower(task)

	for _, model := range ModelDatabase {
		if currentHardware.VRAM_GB < model.HardwareReq.MinVRAM_GB || currentHardware.RAM_GB < model.HardwareReq.MinRAM_GB {
			continue
		}

		if task != "" {
			isSuitable := false
			for _, t := range model.Tasks {
				if strings.Contains(t, task) {
					isSuitable = true
					break
				}
			}
			if !isSuitable {
				continue
			}
		}
		results = append(results, model)
	}
	return results
}

// --- Logging Middleware ---

// loggingMiddleware wraps an http.Handler to log details about the request and its processing time.
func loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// 1. Log request details BEFORE the handler runs
		log.Printf("➡️ START: %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)

		// 2. Call the next handler in the chain
		next.ServeHTTP(w, r)

		// 3. Log request details AFTER the handler runs
		log.Printf("⬅️ END: %s %s processed in %v", r.Method, r.URL.Path, time.Since(start))
	}
}


// --- API Handler ---

func handleRecommendations(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")

	vramStr := r.URL.Query().Get("vram")
	ramStr := r.URL.Query().Get("ram")
	task := r.URL.Query().Get("task")

	vram, err := strconv.Atoi(vramStr)
	if err != nil {
		vram = 8
	}
	ram, err := strconv.Atoi(ramStr)
	if err != nil {
		ram = 16
	}
    
    currentHardware := CurrentHardwareSpecs{VRAM_GB: vram, RAM_GB: ram}

	recommendations := recommendModels(currentHardware, task)

	responsePayload := map[string]interface{}{
		"current_hardware": map[string]string{
			"vram": fmt.Sprintf("%d GB (Manual Input)", currentHardware.VRAM_GB),
			"ram":  fmt.Sprintf("%d GB (Manual Input)", currentHardware.RAM_GB),
		},
		"recommendations": recommendations,
	}

	if err := json.NewEncoder(w).Encode(responsePayload); err != nil {
		log.Printf("Error encoding response: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// --- Web UI Handler (Omitted for brevity, assumed unchanged) ---

var webTemplate = template.Must(template.New("ui").Parse(`
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>LLM Recommender Dev UI</title>
    <style>
        body { font-family: Arial, sans-serif; margin: 20px; background-color: #f4f4f4; color: #333; }
        h1, h2 { color: #0056b3; }
        .container { background-color: #fff; padding: 20px; border-radius: 8px; box-shadow: 0 4px 6px rgba(0,0,0,0.1); }
        .hardware-info { background-color: #e9ecef; padding: 15px; border-radius: 4px; margin-bottom: 20px; display: flex; flex-direction: column; gap: 10px; }
        .input-group { display: flex; align-items: center; gap: 10px; }
        .input-group label { font-weight: bold; }
        .input-group input, .input-group select { padding: 5px; border: 1px solid #ccc; border-radius: 3px; }
        .input-group input[type="number"] { width: 80px; text-align: right; }
        .input-group select { width: 150px; }
        button { padding: 8px 15px; background-color: #28a745; color: white; border: none; border-radius: 4px; cursor: pointer; }
        table { width: 100%; border-collapse: collapse; margin-top: 15px; }
        th, td { border: 1px solid #ddd; padding: 8px; text-align: left; }
        th { background-color: #007bff; color: white; }
    </style>
</head>
<body>

<div class="container">
    <h1>LLM Recommender Dev Interface</h1>

    <div class="hardware-info">
        <h2>Simulated Hardware Profile & Filters</h2>
        <div class="input-group">
            <label for="vram">VRAM (GPU Memory):</label>
            <input type="number" id="vram" value="8" min="1">
            <label for="ram">RAM (System Memory):</label>
            <input type="number" id="ram" value="16" min="1">
            
            <label for="task">Filter by Task:</label>
            <select id="task">
                <option value="">-- All Tasks --</option>
                {{range .UniqueTasks}}
                    <option value="{{.}}">{{.}}</option>
                {{end}}
            </select>
            
            <button type="button" onclick="fetchRecommendations()">Get Recommendations</button>
        </div>
        <p style="font-size:0.9em; margin-top: 10px;" id="status-message">
            Enter hardware details above and click 'Get Recommendations'. Defaults are 8 GB VRAM / 16 GB RAM.
        </p>
    </div>

    <h2>Recommended Models</h2>
    <table id="recommendations-table">
        <thead>
            <tr>
                <th>Model</th>
                <th>Description</th>
                <th>Tasks</th>
                <th>Min VRAM (GB)</th>
                <th>Min RAM (GB)</th>
            </tr>
        </thead>
        <tbody>
            </tbody>
    </table>
</div>

<script>
    const API_URL = "/api/v1/recommendations";

    async function fetchRecommendations() {
        const vramInput = document.getElementById('vram').value;
        const ramInput = document.getElementById('ram').value;
        const taskInput = document.getElementById('task').value; // Changed to select element

        const statusMessage = document.getElementById('status-message');

        const params = new URLSearchParams();
        params.append('vram', vramInput || '8');
        params.append('ram', ramInput || '16');
        if (taskInput) {
            params.append('task', taskInput);
        }

        const url = API_URL + "?" + params.toString();
        statusMessage.innerHTML = 'Fetching recommendations...';

        try {
            const response = await fetch(url);
            const data = await response.json();
            
            // Update status message with actual query data
            const hw = data.current_hardware;
            const taskText = taskInput ? ' for task "' + taskInput + '"' : '';
            statusMessage.innerHTML = 'Recommendations filtered using VRAM: <strong>' + hw.vram + '</strong>, RAM: <strong>' + hw.ram + '</strong>' + taskText + '.';

            // Display Recommendations
            const tbody = document.getElementById('recommendations-table').querySelector('tbody');
            tbody.innerHTML = ''; // Clear previous results

            if (data.recommendations && data.recommendations.length > 0) {
                data.recommendations.forEach(model => {
                    const row = tbody.insertRow();
                    row.insertCell().textContent = model.name;
                    row.insertCell().textContent = model.description;
                    row.insertCell().textContent = model.tasks.join(', ');
                    row.insertCell().textContent = model.hardware_req.min_vram_gb;
                    row.insertCell().textContent = model.hardware_req.min_ram_gb;
                });
            } else {
                const row = tbody.insertCell();
                row.colSpan = 5;
                row.textContent = "No recommended models found for the given criteria.";
            }

        } catch (error) {
            console.error("Error fetching recommendations:", error);
            statusMessage.innerHTML = 'Error fetching data from the API service. Check console for details.';
        }
    }

    // Load initial data on page load
    window.onload = fetchRecommendations;
</script>

</body>
</html>
`))

func handleWebUI(w http.ResponseWriter, r *http.Request) {
	// 1. Get the list of tasks from the database (now includes tasks from dynamically loaded models)
	data := TemplateData{
		UniqueTasks: getUniqueTasks(),
	}

	// 2. Execute the template with the task list
	if err := webTemplate.Execute(w, data); err != nil {
		log.Printf("Error executing template: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// --- Main Server Logic ---

func main() {
	// Initialize ModelDatabase by fetching models and merging metadata
	fetchAndMergeModels()

	port := os.Getenv("RECOMMENDER_PORT")
	if port == "" {
		port = "8081"
	}

	// Handler registrations - Now wrapped with loggingMiddleware
	http.HandleFunc("/", loggingMiddleware(handleWebUI))
	http.HandleFunc("/api/v1/recommendations", loggingMiddleware(handleRecommendations))

	log.Printf("--- LLM Recommender Service Starting ---")
	log.Printf("Web UI available at: http://localhost:%s/", port)
	log.Printf("API Endpoint: http://localhost:%s/api/v1/recommendations", port)

	log.Fatal(http.ListenAndServe(":"+port, nil))
}
