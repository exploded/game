package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
)

func TestLiveBanner(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.Exec(`INSERT INTO live_events (id, platform, url, embed_url, title, scheduled_at, status, source) VALUES
		(7, 'tiktok', 'https://www.tiktok.com/@rob_goudie/live', '', 'Markets this week', datetime('now'), 'live', 'tiktok')`); err != nil {
		t.Fatal(err)
	}
	h := New(db.New(d), d, loadTestTemplates(t), false, time.UTC)
	get := func(etag string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/live/banner", nil)
		if etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
		w := httptest.NewRecorder()
		h.LiveBanner(w, req)
		return w
	}

	w := get("")
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.Contains(body, `href="/learn/live/7"`) || !strings.Contains(body, "Rob is live") || !strings.Contains(body, "Markets this week") {
		t.Fatalf("on air: %d\n%s", w.Code, body)
	}
	etag := w.Header().Get("ETag")
	if etag == "" || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("headers: %v", w.Header())
	}

	if w := get(etag); w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatalf("unchanged: %d %q", w.Code, w.Body)
	}

	// Within the TTL the answer comes from memory, even after the stream ends.
	if _, err := d.Exec(`UPDATE live_events SET status = 'ended'`); err != nil {
		t.Fatal(err)
	}
	if w := get(etag); w.Code != http.StatusNotModified {
		t.Fatalf("cached: %d", w.Code)
	}

	// Once it expires, the banner goes.
	h.liveCache.at = time.Now().Add(-liveBannerTTL)
	w = get(etag)
	if w.Code != http.StatusOK || w.Body.Len() != 0 || w.Header().Get("ETag") == etag {
		t.Fatalf("off air: %d %q etag %q", w.Code, w.Body, w.Header().Get("ETag"))
	}
}

func TestLiveBannerSlot(t *testing.T) {
	srv := learnServer(t)
	// Pages carry the slot app.js fills, with the banner already in it.
	w := as(srv, "", "GET", "/privacy", nil, false)
	if !strings.Contains(w.Body.String(), `<div id="live-slot" data-live-poll="/live/banner"><a href="/learn/live/2" class="live-banner">`) {
		t.Errorf("/privacy has no live slot:\n%s", w.Body)
	}
	// The Live pages show the stream themselves: no banner, no checking.
	w = as(srv, "", "GET", "/learn/live", nil, false)
	if strings.Contains(w.Body.String(), "data-live-poll") {
		t.Error("/learn/live checks for the banner")
	}
}
