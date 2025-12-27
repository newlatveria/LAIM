
from pathlib import Path
import textwrap
import json

ROOT = Path(".")
RAG_DIR = ROOT / "rag"
RAG_DIR.mkdir(exist_ok=True)

# ---------- Add RAG helper ----------

(ROOT / "internal/handlers/rag.go").write_text(textwrap.dedent("""\
package handlers

import (
	"bytes"
	"encoding/json"
	"io"
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
	f, err := os.Open("rag/index.json")
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chunks []RagChunk
	json.NewDecoder(f).Decode(&chunks)
	return chunks, nil
}

func saveIndex(chunks []RagChunk) error {
	f, err := os.Create("rag/index.json")
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
"""))

# ---------- Extend Upload to index text ----------

handlers = ROOT / "internal/handlers/handlers.go"
txt = handlers.read_text()

if "index.json" not in txt:
	txt = txt.replace(
		'json.NewEncoder(w).Encode(map[string]any{',
		textwrap.dedent("""\
			// --- RAG indexing ---
			var chunks []RagChunk
			if existing, err := loadIndex(); err == nil {
				chunks = existing
			}

			for _, f := range files {
				if strings.HasSuffix(f.Filename, ".txt") {
					b, _ := os.ReadFile("uploads/" + filepath.Base(f.Filename))
					for _, c := range chunkText(string(b)) {
						emb, err := embed(h.cfg, c)
						if err == nil {
							chunks = append(chunks, RagChunk{Text: c, Embedding: emb})
						}
					}
				}
			}
			saveIndex(chunks)

		""") + 'json.NewEncoder(w).Encode(map[string]any{'
	)

	handlers.write_text(txt)

# ---------- Add RAG endpoint ----------

(ROOT / "internal/handlers/rag_action.go").write_text(textwrap.dedent("""\
package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
)

func (h *Handlers) Rag(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model  string `json:"model"`
		Prompt string `json:"prompt"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	queryEmb, err := embed(h.cfg, req.Prompt)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	chunks, _ := loadIndex()

	type scored struct {
		Text  string
		Score float64
	}
	var scoredChunks []scored

	for _, c := range chunks {
		scoredChunks = append(scoredChunks, scored{
			Text:  c.Text,
			Score: cosine(queryEmb, c.Embedding),
		})
	}

	// pick top 3
	context := ""
	for i := 0; i < 3 && i < len(scoredChunks); i++ {
		context += scoredChunks[i].Text + "\n"
	}

	payload := map[string]any{
		"model":  req.Model,
		"prompt": "Context:\n" + context + "\n\nQuestion:\n" + req.Prompt,
	}

	buf := new(bytes.Buffer)
	json.NewEncoder(buf).Encode(payload)

	resp, err := http.Post(h.cfg.OllamaBaseURL+"/api/generate", "application/json", buf)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	io.Copy(w, resp.Body)
}
"""))

# ---------- Wire route ----------

main = ROOT / "cmd/webolla/main.go"
main.write_text(main.read_text().replace(
	'mux.HandleFunc("/api/generate", h.Generate)',
	'mux.HandleFunc("/api/generate", h.Generate)\n\tmux.HandleFunc("/api/rag", h.Rag)'
))

print("✅ RAG from uploads added successfully")
