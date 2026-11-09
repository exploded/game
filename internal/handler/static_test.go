package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/exploded/game"
)

func newTestStatic(t *testing.T) *Static {
	t.Helper()
	s, err := NewStatic(fstest.MapFS{
		"css/app.css": {Data: []byte("body{}")},
		"favicon.ico": {Data: []byte("ico")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestStaticURLCarriesContentHash(t *testing.T) {
	s := newTestStatic(t)
	url := s.URL("css/app.css")
	if !strings.HasPrefix(url, "/static/css/app.css?v=") || len(url) != len("/static/css/app.css?v=")+12 {
		t.Errorf("URL = %q, want /static/css/app.css?v=<12 hex>", url)
	}
}

func TestStaticCaching(t *testing.T) {
	s := newTestStatic(t)
	versioned := s.URL("css/app.css")
	hash := versioned[strings.Index(versioned, "=")+1:]

	for _, tc := range []struct {
		name, url, cache string
		status           int
	}{
		{"current hash is immutable", versioned, "public, max-age=31536000, immutable", 200},
		{"no hash must revalidate", "/static/css/app.css", "no-cache", 200},
		{"stale hash must revalidate", "/static/css/app.css?v=000000000000", "no-cache", 200},
		{"directory is not listed", "/static/css/", "", 404},
		{"unknown file", "/static/nope.js", "", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest("GET", tc.url, nil))
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d", w.Code, tc.status)
			}
			if got := w.Header().Get("Cache-Control"); tc.cache != "" && got != tc.cache {
				t.Errorf("Cache-Control = %q, want %q", got, tc.cache)
			}
		})
	}

	// A revalidation with the current ETag is a 304 with no body.
	req := httptest.NewRequest("GET", "/static/css/app.css", nil)
	req.Header.Set("If-None-Match", `"`+hash+`"`)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusNotModified {
		t.Errorf("revalidation status = %d, want 304", w.Code)
	}
}

// TestIntroVideoServes: the site tour plays from the binary as video/mp4,
// and a phone can ask for part of it (iOS won't play a video otherwise).
func TestIntroVideoServes(t *testing.T) {
	s, err := NewStatic(game.Static())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", s.URL("video/the-investor-project-tour.mp4"), nil)
	req.Header.Set("Range", "bytes=0-1023")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.Len() != 1024 || w.Header().Get("Content-Type") != "video/mp4" {
		t.Errorf("GET with Range = %d, %d bytes, %q; want 206, 1024 bytes, video/mp4", w.Code, w.Body.Len(), w.Header().Get("Content-Type"))
	}
	if _, ok := s.hashes["video/the-investor-project-tour.jpg"]; !ok {
		t.Error("the tour's cover image is missing")
	}
}
