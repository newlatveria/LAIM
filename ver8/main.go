package main

import (
	"log"
	"net"
	"net/http"
	"time"

	"webolla/internal/config"
	"webolla/internal/handlers"
)

// getHostIPs finds all non-loopback IPv4 addresses for this machine.
func getHostIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}

	for _, iface := range ifaces {
		// Skip down interfaces or loopback interfaces
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// Ensure it's a valid IPv4 address and not loopback
			if ip == nil || ip.IsLoopback() {
				continue
			}
			if ip.To4() != nil {
				ips = append(ips, ip.String())
			}
		}
	}
	return ips
}

// Custom ResponseWriter to capture status code AND support Flushing (Streaming)
type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

// THIS IS THE FIX: Add the Flush method so the middleware doesn't break streaming
func (lrw *loggingResponseWriter) Flush() {
	if f, ok := lrw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// loggingMiddleware logs details about every incoming request.
func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		lrw := &loggingResponseWriter{
			ResponseWriter: w,
			statusCode:     http.StatusOK, // Default to 200
		}

		next.ServeHTTP(lrw, r)

		log.Printf(
			"%s %s %d %s | %s",
			r.Method,
			r.URL.Path,
			lrw.statusCode,
			time.Since(start),
			r.RemoteAddr,
		)
	})
}

func main() {
	// 1. Load configuration
	cfg := config.Load()
	h := handlers.New(cfg)

	// 2. Initialize Router
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/models", h.Models)
	mux.HandleFunc("/api/generate", h.Generate)
	mux.HandleFunc("/api/rag", h.Rag)
	mux.HandleFunc("/api/upload", h.Upload)
	mux.HandleFunc("/api/reindex", h.ReindexAll)
	mux.HandleFunc("/api/telemetry", h.Telemetry)

	// Static assets (if you have a folder for CSS/JS)
	// mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Catch-all route for the Frontend (must be "/")
	// ServeMux uses longest-prefix matching, so API routes above take priority.
	mux.HandleFunc("/", h.Index)

	// 3. Wrap with Middleware
	handler := loggingMiddleware(mux)

	// 4. Display IP Information
	log.Println("WebOlla UI is accessible at:")
	log.Printf("  Local:   http://localhost:%s\n", cfg.Port)
	for _, ip := range getHostIPs() {
		log.Printf("  Network: http://%s:%s\n", ip, cfg.Port)
	}

	// 5. Start Server
	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Minute, // High timeout for long LLM generations
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %s", err)
	}
}
