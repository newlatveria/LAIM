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
