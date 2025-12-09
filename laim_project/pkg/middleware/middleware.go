package middleware

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"time"
)

func newTraceID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func WithTraceAndTiming(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		trace := newTraceID()
		ctx := context.WithValue(r.Context(), "trace", trace)
		r = r.WithContext(ctx)

		start := time.Now()
		log.Printf("[INFO] Incoming %s %s", r.Method, r.URL.Path)

		next(w, r)

		elapsed := time.Since(start)
		log.Printf("[INFO] Request finished in %d ms", elapsed.Milliseconds())
	}
}
