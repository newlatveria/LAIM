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
