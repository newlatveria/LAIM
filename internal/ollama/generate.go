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
