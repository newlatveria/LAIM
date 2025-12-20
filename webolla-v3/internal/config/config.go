package config
import "os"
type Config struct {
	Port, OllamaBaseURL, UploadDir string
}
func Load() *Config {
	return &Config{
		Port:          env("PORT", "8080"),
		OllamaBaseURL: env("OLLAMA_BASE_URL", "http://127.0.0.1:11434"),
		UploadDir:     env("UPLOAD_DIR", "uploads"),
	}
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" { return v }
	return d
}
