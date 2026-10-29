package handler

import (
	"context"
	"crypto/rand"
	"encoding/base64"
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
// HSTS is only sent when SecurityHeadersProd is set.
var SecurityHeadersProd bool

type nonceKey struct{}

// SecurityHeaders sets the CSP with a fresh nonce per request. Inline
// <script> blocks must carry nonce="{{.Nonce}}" to run, and inline event
// handlers (onclick=...) are blocked outright: put behaviour in app.js.
// Inline style="..." attributes are still allowed ('unsafe-inline' in style-src).
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		nonce := base64.StdEncoding.EncodeToString(b)

		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if SecurityHeadersProd {
			w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; "+
				"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; "+
				"font-src https://fonts.gstatic.com; "+
				"img-src 'self' https://*.googleusercontent.com https://api.twelvedata.com; "+
				"script-src 'self' 'nonce-"+nonce+"' https://static.cloudflareinsights.com; "+
				"connect-src 'self'; "+
				"base-uri 'self'; "+
				"form-action 'self'; "+
				"frame-ancestors 'none'")
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), nonceKey{}, nonce)))
	})
}

// cspNonce returns the request's CSP nonce, set by SecurityHeaders.
func cspNonce(r *http.Request) string {
	nonce, _ := r.Context().Value(nonceKey{}).(string)
	return nonce
}
