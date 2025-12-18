package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port            string
	OllamaBaseURL   string
	GenerateTimeout time.Duration

	GenerateURL string
	ChatURL     string
	TagsURL     string
	PullURL     string
	DeleteURL   string
}

func Load() *Config {
	port := getEnv("PORT", "8080")
	base := getEnv("OLLAMA_BASE_URL", "http://localhost:11434")
	timeoutSec, _ := strconv.Atoi(getEnv("GENERATE_TIMEOUT_SEC", "300"))

	cfg := &Config{
		Port:            port,
		OllamaBaseURL:   base,
		GenerateTimeout: time.Duration(timeoutSec) * time.Second,
	}

	cfg.GenerateURL = base + "/api/generate"
	cfg.ChatURL = base + "/api/chat"
	cfg.TagsURL = base + "/api/tags"
	cfg.PullURL = base + "/api/pull"
	cfg.DeleteURL = base + "/api/delete"

	return cfg
}

func getEnv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
