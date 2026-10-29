package handler

import (
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	d := openTestDB(t)
	h := &Handler{rawDB: d}

	w := httptest.NewRecorder()
	h.Health(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Errorf("healthy: got %d %q, want 200 \"ok\"", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	d.Close()
	w = httptest.NewRecorder()
	h.Health(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 503 {
		t.Errorf("closed db: got %d, want 503", w.Code)
	}
}
