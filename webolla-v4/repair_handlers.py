from pathlib import Path
import textwrap

handlers = Path("internal/handlers/handlers.go")

handlers.write_text(textwrap.dedent("""\
    package handlers

    import (
        "bytes"
        "encoding/json"
        "io"
        "net/http"
        "os"
        "path/filepath"

        "webolla/internal/config"
    )

    type Handlers struct {
        cfg *config.Config
    }

    func New(cfg *config.Config) *Handlers {
        return &Handlers{cfg: cfg}
    }

    // Serves UI
    func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
        http.ServeFile(w, r, "web/index.html")
    }

    // Lists Ollama models
    func (h *Handlers) Models(w http.ResponseWriter, r *http.Request) {
        resp, err := http.Get(h.cfg.OllamaBaseURL + "/api/tags")
        if err != nil {
            http.Error(w, err.Error(), 500)
            return
        }
        defer resp.Body.Close()
        w.Header().Set("Content-Type", "application/json")
        io.Copy(w, resp.Body)
    }

    // Non-streaming generate
    func (h *Handlers) Generate(w http.ResponseWriter, r *http.Request) {
        var req map[string]any
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            http.Error(w, err.Error(), 400)
            return
        }

        buf := new(bytes.Buffer)
        json.NewEncoder(buf).Encode(req)

        resp, err := http.Post(
            h.cfg.OllamaBaseURL + "/api/generate",
            "application/json",
            buf,
        )
        if err != nil {
            http.Error(w, err.Error(), 500)
            return
        }
        defer resp.Body.Close()

        w.Header().Set("Content-Type", "application/json")
        io.Copy(w, resp.Body)
    }

    // File + folder upload
    func (h *Handlers) Upload(w http.ResponseWriter, r *http.Request) {
        if err := r.ParseMultipartForm(64 << 20); err != nil {
            http.Error(w, err.Error(), 400)
            return
        }

        files := r.MultipartForm.File["files"]
        os.MkdirAll("uploads", 0755)

        for _, f := range files {
            src, err := f.Open()
            if err != nil {
                continue
            }
            defer src.Close()

            dst, err := os.Create(filepath.Join("uploads", filepath.Base(f.Filename)))
            if err != nil {
                continue
            }
            io.Copy(dst, src)
            dst.Close()
        }

        json.NewEncoder(w).Encode(map[string]any{
            "uploaded": len(files),
        })
    }
"""))

print("✅ handlers.go repaired and aligned with main.go")
