package config

import "os"

type Config struct {
	Port           string
	OllamaBaseURL  string
	UploadDir      string
}

func Load() *Config {
	return &Config{
		Port:          getEnv("PORT", "8080"),
		OllamaBaseURL: getEnv("OLLAMA_URL", "http://localhost:11434"),
		UploadDir:     "./uploads",
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
