package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestStockNewsThumbs: a shown story with a picture gets it beside its
// headline, served from our own site with a year's cache; a story whose
// picture couldn't be had (empty data) shows none, and its address is a 404.
func TestStockNewsThumbs(t *testing.T) {
	srv, _, d := gameServerDB(t)
	for _, stmt := range []string{
		`INSERT INTO stock_profiles (stock_id, description) VALUES (1, 'Makes anvils.')`, // the news sits in the profile part of the page
		`INSERT INTO stock_news (id, stock_id, headline, source, url, published_at, shown) VALUES
			(1, 1, 'With picture', 'cnbc.com', 'https://cnbc.com/a', 200, 1),
			(2, 1, 'Without picture', 'npr.org', 'https://npr.org/b', 100, 1)`,
		`INSERT INTO news_thumbs (news_id, content_type, data) VALUES (1, 'image/jpeg', X'FFD8FFD9'), (2, '', X'')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	page := getPage(t, srv, "/games/1/stocks/1", "1")
	if !strings.Contains(page, `<img src="/news-thumb/1"`) {
		t.Error("the story with a picture doesn't show it")
	}
	if strings.Contains(page, `/news-thumb/2"`) {
		t.Error("the story without a picture asks for one")
	}
	if n := strings.Count(page, `class="has-thumb"`); n != 1 {
		t.Errorf("%d stories marked has-thumb, want 1", n)
	}

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/news-thumb/1", nil))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/jpeg" ||
		!strings.Contains(w.Header().Get("Cache-Control"), "immutable") || w.Body.Len() != 4 {
		t.Errorf("/news-thumb/1: %d %q %q, %d bytes", w.Code, w.Header().Get("Content-Type"), w.Header().Get("Cache-Control"), w.Body.Len())
	}
	for _, path := range []string{"/news-thumb/2", "/news-thumb/99"} {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", path, w.Code)
		}
	}
}
