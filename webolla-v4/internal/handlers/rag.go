package handlers

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"os"
	"strings"

	"webolla/internal/config"
)

type RagChunk struct {
	Text      string    `json:"text"`
	Embedding []float64 `json:"embedding"`
}

func embed(cfg *config.Config, text string) ([]float64, error) {
	payload := map[string]any{
		"model": "nomic-embed-text",
		"input": text,
	}

	buf := new(bytes.Buffer)
	json.NewEncoder(buf).Encode(payload)

	resp, err := http.Post(cfg.OllamaBaseURL+"/api/embeddings", "application/json", buf)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out struct {
		Embedding []float64 `json:"embedding"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	return out.Embedding, nil
}

func cosine(a, b []float64) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func loadIndex() ([]RagChunk, error) {
	f, err := os.Open("data/rag/index.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chunks []RagChunk
	json.NewDecoder(f).Decode(&chunks)
	return chunks, nil
}

func saveIndex(chunks []RagChunk) error {
	f, err := os.Create("data/rag/index.json")
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(chunks)
}

func chunkText(text string) []string {
	words := strings.Fields(text)
	var chunks []string
	for i := 0; i < len(words); i += 200 {
		end := i + 200
		if end > len(words) {
			end = len(words)
		}
		chunks = append(chunks, strings.Join(words[i:end], " "))
	}
	return chunks
}
