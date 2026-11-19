package handler

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// RateLimiter provides simple per-IP rate limiting using a sliding window.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	limit    int
	window   time.Duration
}

type visitor struct {
	count   int
	resetAt time.Time
}

// NewRateLimiter creates a rate limiter that allows limit requests per window per IP.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		limit:    limit,
		window:   window,
	}
	// Background cleanup of stale entries every minute.
	go func() {
		for {
			time.Sleep(time.Minute)
			rl.mu.Lock()
			now := time.Now()
			for ip, v := range rl.visitors {
				if now.After(v.resetAt) {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

// Allow checks whether a request from the given IP is allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	v, ok := rl.visitors[ip]
	if !ok || now.After(v.resetAt) {
		rl.visitors[ip] = &visitor{count: 1, resetAt: now.Add(rl.window)}
		return true
	}
	v.count++
	return v.count <= rl.limit
}

// Middleware returns HTTP middleware that rate-limits requests by IP.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.Allow(clientIP(r)) {
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP is the visitor's address, the rate limiters' key.
//
// It trusts X-Forwarded-For, which anyone can send. That's safe only because
// of how production is set up: the app listens on 127.0.0.1 (BIND_ADDR,
// main.go), so only Caddy on the same box reaches it, and Caddy's site block
// sets the header to a single address with `header_up X-Forwarded-For
// {client_ip}`, replacing whatever the client sent. Caddy works out
// {client_ip} from Cloudflare's X-Forwarded-For only for connections from
// Cloudflare's ranges (`trusted_proxies` and `trusted_proxies_strict` in the
// global options of /etc/caddy/Caddyfile). Change any of that and a client
// can pick its own key, and so skip the limits. Without the header (dev, or
// a direct connection) it's the connection's address, without the port.
func clientIP(r *http.Request) string {
	if fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); fwd != "" {
		return fwd
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
