package ollama

import (
	"net/http"

	"webolla/internal/config"
)

type Client struct {
	HTTP *http.Client
	Cfg  *config.Config
}

func NewClient(cfg *config.Config) *Client {
	return &Client{
		Cfg: cfg,
		HTTP: &http.Client{
			Timeout: 0, // streaming requests manage timeouts via context
		},
	}
}
