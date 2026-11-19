package handler

import (
	"net/http/httptest"
	"testing"
	"time"
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

	// A scheduler that has stopped ticking reads as down; one ticking, or
	// just started, reads as up.
	var last time.Time
	h.SetHeartbeat(func() time.Time { return last })
	for _, c := range []struct {
		name  string
		since time.Duration // how long ago the heartbeat was wired in
		last  time.Duration // how long ago the last tick was; 0 for none
		want  int
	}{
		{"just started, no tick yet", 0, 0, 200},
		{"ticked a minute ago", time.Hour, time.Minute, 200},
		{"no tick for 11 minutes", time.Hour, 11 * time.Minute, 503},
		{"started 11 minutes ago, never ticked", 11 * time.Minute, 0, 503},
	} {
		h.heartbeat.since = time.Now().Add(-c.since)
		last = time.Time{}
		if c.last != 0 {
			last = time.Now().Add(-c.last)
		}
		w = httptest.NewRecorder()
		h.Health(w, httptest.NewRequest("GET", "/health", nil))
		if w.Code != c.want {
			t.Errorf("%s: got %d, want %d", c.name, w.Code, c.want)
		}
	}
	h.heartbeat = heartbeatSource{}

	d.Close()
	w = httptest.NewRecorder()
	h.Health(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 503 {
		t.Errorf("closed db: got %d, want 503", w.Code)
	}
}
