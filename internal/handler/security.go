package handler

import (
	"log/slog"
	"net/http"
	"time"
)

// RequestLogger logs method, path, status, and duration for every request.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", sw.status,
			"duration", time.Since(start),
		)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// SecurityHeaders adds security-related headers to every response.
// Production mode is read from the handler via the request context.
var SecurityHeadersProd bool

func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if SecurityHeadersProd {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"style-src 'self' https://fonts.googleapis.com; "+
				"font-src https://fonts.gstatic.com; "+
				"img-src 'self' https://*.googleusercontent.com; "+
				"script-src 'self' 'unsafe-inline'")
		next.ServeHTTP(w, r)
	})
}
