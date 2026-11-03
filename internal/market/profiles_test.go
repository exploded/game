package market

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestLogoLoads keeps a logo that serves an image and drops one that 404s.
// A logo host that can't be reached counts as loading, so a network blip
// doesn't clear a stored logo.
func TestLogoLoads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/logo/apple.com" {
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("png"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := context.Background()
	if !logoLoads(ctx, srv.URL+"/logo/apple.com") {
		t.Error("a logo that loads was dropped")
	}
	if logoLoads(ctx, srv.URL+"/logo/corporate.lululemon.com") {
		t.Error("a logo that 404s was kept")
	}
	if !logoLoads(ctx, "http://127.0.0.1:9/logo/apple.com") {
		t.Error("a network error dropped the logo")
	}
}
