package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// holdDashboard holds each /dashboard request until release has a value or
// is closed, so a test can fill the throttle's places. Other paths answer at
// once.
func holdDashboard(started chan<- struct{}, release <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dashboard" {
			started <- struct{}{}
			<-release
		}
		w.WriteHeader(http.StatusOK)
	})
}

func TestThrottleRefusesOverTheCap(t *testing.T) {
	started, release := make(chan struct{}, 10), make(chan struct{})
	defer close(release)
	h := NewThrottle(1, 0, time.Second, time.Minute).Middleware(holdDashboard(started, release))

	// The one place is taken.
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/dashboard", nil))
	<-started

	// A page load gets the busy page at once, with Retry-After.
	req := httptest.NewRequest("GET", "/games/1", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	w := httptest.NewRecorder()
	begin := time.Now()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || w.Header().Get("Retry-After") != "30" {
		t.Fatalf("over the cap: got %d, Retry-After %q; want 503 and 30", w.Code, w.Header().Get("Retry-After"))
	}
	if time.Since(begin) > 200*time.Millisecond {
		t.Errorf("refusal took %s; with no backlog it should be at once", time.Since(begin))
	}
	if body := w.Body.String(); !strings.Contains(body, "The Wealth Lab is busy") || strings.Contains(body, "<script") {
		t.Errorf("busy page: %q", body)
	}

	// An htmx request gets text for the toast, not a page.
	req = httptest.NewRequest("GET", "/games/1/stocks/2/price", nil)
	req.Header.Set("HX-Request", "true")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || strings.Contains(w.Body.String(), "<html") {
		t.Errorf("htmx over the cap: got %d %q", w.Code, w.Body.String())
	}

	// Static files, the uptime check and AI listen streams aren't capped.
	for _, path := range []string{"/static/css/app.css", "/favicon.ico", "/health", "/mcp"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("%s while full: got %d, want 200", path, w.Code)
		}
	}
}

func TestThrottleBacklogWaitsThenGivesUp(t *testing.T) {
	started, release := make(chan struct{}, 10), make(chan struct{})
	defer close(release)
	h := NewThrottle(1, 1, 50*time.Millisecond, time.Minute).Middleware(holdDashboard(started, release))
	go h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/dashboard", nil))
	<-started

	// A second request waits for a place, then is refused.
	w := httptest.NewRecorder()
	begin := time.Now()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/games/1", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d, want 503", w.Code)
	}
	if waited := time.Since(begin); waited < 40*time.Millisecond {
		t.Errorf("refused after %s; it should wait about 50ms for a place", waited)
	}

	// One that's waiting when the place frees up is served.
	done := make(chan int)
	go func() {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/games/1", nil))
		done <- w.Code
	}()
	time.Sleep(10 * time.Millisecond)
	release <- struct{}{} // the held request finishes
	if code := <-done; code != http.StatusOK {
		t.Errorf("waiting request: got %d, want 200", code)
	}
}

func TestThrottleSetsADeadline(t *testing.T) {
	var deadline time.Time
	var ok bool
	h := NewThrottle(4, 4, time.Second, 50*time.Second).Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deadline, ok = r.Context().Deadline()
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/dashboard", nil))
	if !ok || time.Until(deadline) < 45*time.Second || time.Until(deadline) > 50*time.Second {
		t.Errorf("deadline %v (set %v); want about 50s from now", deadline, ok)
	}
}
