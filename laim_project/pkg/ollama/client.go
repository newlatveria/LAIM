package ollama

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/ollama/ollama/api"
)

func CallOllama(model string, msgs []api.Message) (string, error) {
	client := api.NewClient(&url.URL{Scheme: "http", Host: "localhost:11434"}, http.DefaultClient)

	req := &api.ChatRequest{
		Model:    model,
		Messages: msgs,
		Stream:   func() *bool { b := true; return &b }(),
	}

	var out strings.Builder

	err := client.Chat(context.Background(), req, func(resp api.ChatResponse) error {
		out.WriteString(resp.Message.Content)
		return nil
	})

	return out.String(), err
}
