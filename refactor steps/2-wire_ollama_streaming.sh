#!/usr/bin/env bash
set -euo pipefail

ROOT="webolla-refactored"

if [[ ! -d "$ROOT/internal/ollama" ]]; then
  echo "❌ Refactored project not found. Run migrate_webolla.sh first."
  exit 1
fi

echo "🔌 Wiring Ollama streaming client..."

############################
# 1. Streaming helper
############################
cat > $ROOT/internal/ollama/stream.go <<'EOF'
package ollama

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type StreamFunc func(chunk OllamaResponseChunk) error

func StreamToSSE(resp *http.Response, w http.ResponseWriter, emit StreamFunc) error {
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return fmt.Errorf("streaming not supported")
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var chunk OllamaResponseChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}

		if err := emit(chunk); err != nil {
			return err
		}

		if chunk.Done {
			fmt.Fprint(w, "data: [DONE]\n\n")
			flusher.Flush()
			return nil
		}

		fmt.Fprintf(w, "data: %s\n\n", line)
		flusher.Flush()
	}

	return scanner.Err()
}
EOF

############################
# 2. Generate streaming
############################
cat > $ROOT/internal/ollama/generate.go <<'EOF'
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

func (c *Client) GenerateStream(
	ctx context.Context,
	w http.ResponseWriter,
	model string,
	prompt string,
	params GenerationParams,
) error {

	options := buildOptions(params)

	reqBody := OllamaGenerateRequestPayload{
		Model:   model,
		Prompt: prompt,
		Stream: true,
		Options: options,
	}

	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.Cfg.GenerateURL,
		bytes.NewBuffer(body),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}

	return StreamToSSE(resp, w, func(chunk OllamaResponseChunk) error {
		return nil
	})
}
EOF

############################
# 3. Chat streaming
############################
cat > $ROOT/internal/ollama/chat.go <<'EOF'
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

func (c *Client) ChatStream(
	ctx context.Context,
	w http.ResponseWriter,
	model string,
	messages []Message,
	params GenerationParams,
) error {

	options := buildOptions(params)

	reqBody := OllamaChatRequestPayload{
		Model:    model,
		Messages: messages,
		Stream:   true,
		Options:  options,
	}

	body, _ := json.Marshal(reqBody)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.Cfg.ChatURL,
		bytes.NewBuffer(body),
	)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}

	return StreamToSSE(resp, w, func(chunk OllamaResponseChunk) error {
		return nil
	})
}
EOF

############################
# 4. Handler wiring
############################
cat > $ROOT/internal/handlers/ollama_action.go <<'EOF'
package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type ClientRequest struct {
	ActionType string           `json:"actionType"`
	Model      string           `json:"model"`
	Prompt     string           `json:"prompt"`
	Messages   []Message        `json:"messages"`
	Params     GenerationParams `json:"params"`
}

func (h *Handlers) OllamaAction(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ClientRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.GenerateTimeout)
	defer cancel()

	switch req.ActionType {
	case "generate":
		_ = h.client.GenerateStream(ctx, w, req.Model, req.Prompt, req.Params)
	case "chat":
		_ = h.client.ChatStream(ctx, w, req.Model, req.Messages, req.Params)
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
	}
}
EOF

echo "✅ Ollama streaming wired"
echo ""
echo "Next steps:"
echo "  - Move pull/delete/list into internal/ollama"
echo "  - Remove *_raw.go files"
echo "  - go build ./..."
