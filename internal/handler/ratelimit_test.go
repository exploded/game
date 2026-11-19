package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimiterAllow(t *testing.T) {
	rl := NewRateLimiter(3, 50*time.Millisecond)
	for i := 1; i <= 3; i++ {
		if !rl.Allow("1.2.3.4") {
			t.Fatalf("request %d refused, want 3 allowed", i)
		}
	}
	if rl.Allow("1.2.3.4") {
		t.Error("request 4 allowed in the same window")
	}
	if !rl.Allow("5.6.7.8") {
		t.Error("another address shares the first one's count")
	}
	time.Sleep(60 * time.Millisecond)
	if !rl.Allow("1.2.3.4") {
		t.Error("still refused after the window ended")
	}
}

// The key is Caddy's X-Forwarded-For (one address, set from Cloudflare's
// header only for Cloudflare's ranges), else the connection's address
// without its port, so a client's new connections share one count.
func TestClientIP(t *testing.T) {
	for _, tc := range []struct{ remote, fwd, want string }{
		{"127.0.0.1:51234", "203.0.113.9", "203.0.113.9"},
		{"127.0.0.1:51234", " 2001:db8::1 ", "2001:db8::1"},
		{"192.0.2.7:51234", "", "192.0.2.7"},
		{"[2001:db8::2]:443", "", "2001:db8::2"},
		{"pipe", "", "pipe"},
	} {
		r := httptest.NewRequest("POST", "/login", nil)
		r.RemoteAddr = tc.remote
		if tc.fwd != "" {
			r.Header.Set("X-Forwarded-For", tc.fwd)
		}
		if got := clientIP(r); got != tc.want {
			t.Errorf("clientIP(%q, XFF %q) = %q, want %q", tc.remote, tc.fwd, got, tc.want)
		}
	}
}

func TestRateLimiterMiddleware(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	srv := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	send := func(remote string) int {
		r := httptest.NewRequest("POST", "/login", nil)
		r.RemoteAddr = remote
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		return w.Code
	}
	// New connections from one address (new ports) share its count.
	for i, port := range []string{"1001", "1002", "1003"} {
		want := http.StatusOK
		if i == 2 {
			want = http.StatusTooManyRequests
		}
		if got := send("192.0.2.7:" + port); got != want {
			t.Errorf("request %d: %d, want %d", i+1, got, want)
		}
	}
}
