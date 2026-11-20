package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// POST /stats/play counts real videos, streams, lessons and the tour, from
// the site itself, signed in or not, and /admin/stats lists them.
func TestStatsPlayAndAdminStats(t *testing.T) {
	srv, d := siteServer(t)
	for _, stmt := range []string{
		`INSERT INTO videos (id, platform, external_id, url, embed_url, title, published_at)
			VALUES (5, 'youtube', 'abc', 'https://youtu.be/abc', 'https://www.youtube-nocookie.com/embed/abc', 'Retiring on $650,000', '2026-11-01 00:00:00')`,
		`INSERT INTO courses (id, slug, title, published) VALUES (1, 'basics', 'Share basics', 1)`,
		`INSERT INTO lessons (id, course_id, title, video_url, embed_url) VALUES (3, 1, 'What is a share?', 'https://vimeo.com/1', 'https://player.vimeo.com/video/1')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	play := func(site string, form url.Values) int {
		req := httptest.NewRequest("POST", "/stats/play", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Sec-Fetch-Site", site)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w.Code
	}
	tests := []struct {
		kind, id string
		want     int
	}{
		{"video", "5", http.StatusNoContent},
		{"video", "5", http.StatusNoContent},
		{"lesson", "3", http.StatusNoContent},
		{"intro", "", http.StatusNoContent},
		{"video", "99", http.StatusNotFound}, // no such video
		{"lesson", "5", http.StatusNotFound},
		{"video", "abc", http.StatusBadRequest},
		{"film", "5", http.StatusBadRequest},
	}
	for _, tt := range tests {
		if got := play("same-origin", url.Values{"kind": {tt.kind}, "id": {tt.id}}); got != tt.want {
			t.Errorf("play %s %q: %d, want %d", tt.kind, tt.id, got, tt.want)
		}
	}
	if got := play("cross-site", url.Values{"kind": {"video"}, "id": {"5"}}); got != http.StatusForbidden {
		t.Errorf("play from another site: %d, want 403", got)
	}

	// Opening a calculator and a course page counts too.
	for _, path := range []string{"/tools/fhss", "/learn/courses/basics"} {
		if w := siteRequest(srv, 2, "GET", path, nil, false); w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d", path, w.Code)
		}
	}

	w := siteRequest(srv, 1, "GET", "/admin/stats", nil, false)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /admin/stats: %d\n%s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, want := range []string{
		"Counts only.",
		`<span class="num">2</span><a href="/learn/v/5">Retiring on $650,000</a>`,
		`<a href="/learn/courses/basics/3">Share basics: What is a share?</a>`,
		`<a href="/tools/fhss">First Home Super Saver Calculator</a>`,
		`<a href="/learn/courses/basics">Share basics</a>`,
		// The day's totals add up every video's plays.
		`<td class="num" data-label="Video plays">2</td>`,
		`<td class="num" data-label="Lesson plays">1</td>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/admin/stats doesn't show %q", want)
		}
	}
	// Nothing about who: the counts table has no user column at all.
	rows, err := d.Query("SELECT name FROM pragma_table_info('stat_counts')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var col string
		rows.Scan(&col)
		if strings.Contains(col, "user") {
			t.Errorf("stat_counts has a column %q", col)
		}
	}
}
