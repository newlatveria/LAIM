#!/usr/bin/env bash
set -euo pipefail

SRC="webolla.go"
OUT="webolla-refactored"

if [[ ! -f "$SRC" ]]; then
  echo "❌ webolla.go not found"
  exit 1
fi

echo "📦 Creating refactored structure..."

mkdir -p $OUT/{cmd/webolla,internal/{config,handlers,ollama},web}

############################
# 1. main.go
############################
cat > $OUT/cmd/webolla/main.go <<'EOF'
package main

import (
	"log"
	"net/http"

	"webolla/internal/config"
	"webolla/internal/handlers"
)

func main() {
	cfg := config.Load()
	h := handlers.New(cfg)

	http.HandleFunc("/", h.ServeHTML)
	http.HandleFunc("/api/ollama-action", h.OllamaAction)
	http.HandleFunc("/api/models", h.ListModels)
	http.HandleFunc("/api/status", h.Status)
	http.HandleFunc("/api/cancel", h.Cancel)

	log.Printf("Server listening on :%s", cfg.Port)
	log.Fatal(http.ListenAndServe(":"+cfg.Port, nil))
}
EOF

############################
# 2. config extraction
############################
echo "🧭 Extracting config..."

awk '
/const \(/,/^\)/ { print }
/func getEnv/,/^}/ { print }
/func init\(/,/^}/ { print }
' "$SRC" > $OUT/internal/config/config_raw.go

cat > $OUT/internal/config/config.go <<'EOF'
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
EOF

echo "   → raw config copied to config_raw.go for reference"

############################
# 3. Ollama types & logic
############################
echo "🤖 Extracting Ollama types..."

awk '
/type GenerationParams/,/type ServerStatus/ { print }
' "$SRC" > $OUT/internal/ollama/types.go

echo "// TODO: move Ollama API call logic here" >> $OUT/internal/ollama/types.go

cat > $OUT/internal/ollama/client.go <<'EOF'
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
EOF

############################
# 4. Handlers
############################
echo "🌐 Extracting handlers..."

awk '
/func handleServerStatus/,/^}/ { print }
/func handleCancelRequest/,/^}/ { print }
/func handleListModels/,/^}/ { print }
/func handleOllamaAction/,/^}/ { print }
' "$SRC" > $OUT/internal/handlers/handlers_raw.go

cat > $OUT/internal/handlers/handlers.go <<'EOF'
package handlers

import (
	"embed"
	"net/http"

	"webolla/internal/config"
	"webolla/internal/ollama"
)

//go:embed ../../web/index.html
var webFS embed.FS

type Handlers struct {
	cfg    *config.Config
	client *ollama.Client
}

func New(cfg *config.Config) *Handlers {
	return &Handlers{
		cfg:    cfg,
		client: ollama.New(cfg),
	}
}

func (h *Handlers) ServeHTML(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	data, _ := webFS.ReadFile("../../web/index.html")
	w.Write(data)
}
EOF

############################
# 5. HTML extraction
############################
echo "🎨 Extracting HTML..."

awk '
/const htmlContent = `/ { flag=1; next }
/`$/ { flag=0 }
flag { print }
' "$SRC" > $OUT/web/index.html

############################
# 6. go.mod
############################
cat > $OUT/go.mod <<'EOF'
module webolla

go 1.22
EOF

echo ""
echo "✅ Migration scaffold complete"
echo ""
echo "Next manual steps:"
echo "  - Move Ollama call logic into internal/ollama"
echo "  - Wire handlers to client methods"
echo "  - Delete *_raw.go files after verification"
echo "  - Run: go build ./..."
