package handler

import (
	"net/http"
)

// MaxBodySize limits the size of incoming request bodies.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// OpenCORS lets any site's scripts call these routes: the API, MCP and OAuth
// endpoints, which take bearer tokens or nothing, never cookies, so another
// site gains nothing a script of its own couldn't do. Browser-based MCP
// clients need it to read the sign-in metadata and the 401's
// WWW-Authenticate header.
func OpenCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Expose-Headers", "WWW-Authenticate, Mcp-Session-Id, Retry-After, Idempotent-Replayed")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, Mcp-Protocol-Version, Mcp-Session-Id, Last-Event-ID")
			w.Header().Set("Access-Control-Max-Age", "86400")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
