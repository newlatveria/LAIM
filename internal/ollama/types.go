package ollama

type GenerationParams struct {
	Temperature  float64 `json:"temperature"`
	TopP         float64 `json:"top_p"`
	TopK         int     `json:"top_k"`
	RepeatPenalty float64 `json:"repeat_penalty"`
	NumPredict   int     `json:"num_predict"`
}

type OllamaGenerateRequestPayload struct {
	Model  string            `json:"model"`
	Prompt string            `json:"prompt"`
	Stream bool              `json:"stream"`
	Options map[string]interface{} `json:"options,omitempty"`
}

type OllamaChatRequestPayload struct {
	Model    string            `json:"model"`
	Messages []Message         `json:"messages"`
	Stream   bool              `json:"stream"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type OllamaModelActionPayload struct {
	Model string `json:"name"`
}

type OllamaResponseChunk struct {
	Model     string    `json:"model"`
	CreatedAt string    `json:"created_at"`
	Response  string    `json:"response"`
	Message   *Message  `json:"message"`
	Done      bool      `json:"done"`
}

type ClientRequest struct {
	ActionType string           `json:"actionType"`
	Model      string           `json:"model"`
	Prompt     string           `json:"prompt"`
	Messages   []Message        `json:"messages"`
	Params     GenerationParams `json:"params"`
}

type OllamaModel struct {
	Name string `json:"name"`
}

type OllamaTagsResponse struct {
	Models []OllamaModel `json:"models"`
}

type ServerStatus struct {
// TODO: move Ollama API call logic here
	OllamaURL    string `json:"ollama_url"`
	Connected    bool   `json:"connected"`
	PortListening string `json:"port"`
}