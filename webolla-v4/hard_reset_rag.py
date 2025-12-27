
from pathlib import Path

rag = Path("internal/handlers/rag_action.go")

rag.write_text("""package handlers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
)

func (h *Handlers) Rag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	queryEmb, err := embed(h.cfg, req.Prompt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	chunks, err := loadIndex()
	if err != nil || len(chunks) == 0 {
		http.Error(w, "no documents indexed", http.StatusBadRequest)
		return
	}

	type scored struct {
		Text  string
		Score float64
	}

	scoredChunks := make([]scored, 0, len(chunks))
	for _, c := range chunks {
		scoredChunks = append(scoredChunks, scored{
			Text:  c.Text,
			Score: cosine(queryEmb, c.Embedding),
		})
	}

	sort.Slice(scoredChunks, func(i, j int) bool {
		return scoredChunks[i].Score > scoredChunks[j].Score
	})

	context := ""
	for i := 0; i < 3 && i < len(scoredChunks); i++ {
		context += scoredChunks[i].Text + "\\n"
	}

	payload := map[string]any{
		"model":  req.Model,
		"prompt": "Context:\\n" + context + "\\n\\nQuestion:\\n" + req.Prompt,
	}

	buf := new(bytes.Buffer)
	_ = json.NewEncoder(buf).Encode(payload)

	resp, err := http.Post(
		h.cfg.OllamaBaseURL+"/api/generate",
		"application/json",
		buf,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}
""")

print("✅ rag_action.go fully reset with valid Go source")
