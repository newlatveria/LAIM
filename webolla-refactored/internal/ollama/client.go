package ollama

import (
	"net/http"
	"time"

	"webolla/internal/config"
)

type Client struct {
	HTTP *http.Client
	Cfg  *config.Config
}

func New(cfg *config.Config) *Client {
	return &Client{
		Cfg: cfg,
		HTTP: &http.Client{
			Timeout: cfg.GenerateTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}
