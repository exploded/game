package media_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/exploded/game/internal/media"
)

// fakeTikTokLive serves TikTok's live room endpoint for Rob's account.
type fakeTikTokLive struct {
	mu       sync.Mutex
	t        *testing.T
	status   int    // the room's status: 2 live, 4 off air
	title    string // the stream's title
	reply    string // sent as is instead, when set
	code     int    // the HTTP status, when not 200
	requests int
}

func (f *fakeTikTokLive) set(status int, reply string, code int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status, f.reply, f.code = status, reply, code
}

func (f *fakeTikTokLive) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests
}

func (f *fakeTikTokLive) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if r.URL.Path != "/api-live/user/room/" || r.URL.Query().Get("uniqueId") != "rob_goudie" || r.UserAgent() == "" {
		f.t.Errorf("request %s, user agent %q", r.URL, r.UserAgent())
	}
	if f.code != 0 {
		w.WriteHeader(f.code)
		return
	}
	if f.reply != "" {
		w.Write([]byte(f.reply))
		return
	}
	fmt.Fprintf(w, `{"data":{"user":{"uniqueId":"rob_goudie","status":%d},"liveRoom":{"title":%q,"status":%d}},"message":"","statusCode":0}`, f.status, f.title, f.status)
}

func liveStatuses(t *testing.T, d *sql.DB) string {
	t.Helper()
	rows, err := d.Query("SELECT status FROM live_events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var st string
		rows.Scan(&st)
		out = append(out, st)
	}
	return strings.Join(out, " ")
}

func TestTikTokLiveCheckBooked(t *testing.T) {
	d, q, m := openDB(t)
	if _, err := d.Exec(`INSERT INTO live_events (id, platform, url, title, scheduled_at, status) VALUES
		(1, 'tiktok', 'https://www.tiktok.com/@rob_goudie/live', 'Booked now', datetime('now', '+5 minutes'), 'upcoming'),
		(2, 'tiktok', 'https://www.tiktok.com/@rob_goudie/live', 'Booked earlier', datetime('now', '-90 minutes'), 'upcoming'),
		(3, 'tiktok', 'https://www.tiktok.com/@rob_goudie/live', 'Booked later', datetime('now', '+3 hours'), 'upcoming'),
		(4, 'facebook', 'https://www.facebook.com/consortiumpw/live', 'Facebook', datetime('now', '+1 minutes'), 'upcoming')`); err != nil {
		t.Fatal(err)
	}
	tt := &fakeTikTokLive{t: t, status: 4}
	srv := httptest.NewServer(tt)
	defer srv.Close()
	s := media.NewTestService(q, m, nil, nil, nil)
	s.UseTikTokLiveServer(srv.URL)
	ctx := context.Background()
	// 2:01 am: the all-day check isn't due, so only booked streams are checked.
	night := time.Date(2026, 10, 31, 2, 1, 0, 0, time.UTC)
	check := func() {
		t.Helper()
		if err := media.CheckTikTokLive(s, ctx, night); err != nil {
			t.Fatal(err)
		}
	}

	check()
	if got := liveStatuses(t, d); got != "upcoming upcoming upcoming upcoming" {
		t.Errorf("off air: statuses = %s", got)
	}

	// Rob goes live: the stream booked nearest to now starts, once.
	tt.set(2, "", 0)
	check()
	check()
	if got := liveStatuses(t, d); got != "live upcoming upcoming upcoming" {
		t.Errorf("on air: statuses = %s", got)
	}

	// A blip off air doesn't end it; five checks off air in a row do.
	tt.set(4, "", 0)
	for range 4 {
		check()
	}
	tt.set(2, "", 0)
	check()
	tt.set(4, "", 0)
	for range 4 {
		check()
	}
	if got := liveStatuses(t, d); got != "live upcoming upcoming upcoming" {
		t.Errorf("after a blip: statuses = %s", got)
	}
	check()
	if got := liveStatuses(t, d); got != "ended upcoming upcoming upcoming" {
		t.Errorf("off air for five checks: statuses = %s", got)
	}

	// A stream started by hand that no check has seen on air is left on.
	d.Exec("UPDATE live_events SET status = 'live', started_at = datetime('now') WHERE id = 2")
	for range 6 {
		check()
	}
	if got := liveStatuses(t, d); got != "ended live upcoming upcoming" {
		t.Errorf("started by hand: statuses = %s", got)
	}

	// Every check is logged, with what it did.
	var n, booked int
	d.QueryRow("SELECT COUNT(*), SUM(booked) FROM tiktok_live_checks").Scan(&n, &booked)
	if n != 19 || booked != 19 {
		t.Errorf("%d checks logged (%d booked), want 19", n, booked)
	}
	var notes []string
	rows, err := d.Query("SELECT detail || ' ' || event_id FROM tiktok_live_checks WHERE detail != '' ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var s string
		rows.Scan(&s)
		notes = append(notes, s)
	}
	rows.Close()
	want := []string{"Started the booked stream. 1", "Ended the stream after 5 checks off air. 1"}
	if !slices.Equal(notes, want) {
		t.Errorf("notes = %q, want %q", notes, want)
	}

	// TikTok sends a challenge page instead: the check fails and says so.
	tt.set(4, "<!DOCTYPE html><html><body>Please wait...</body></html>", 0)
	if err := media.CheckTikTokLive(s, ctx, night); err == nil {
		t.Error("a challenge page didn't fail the check")
	}
	var result, detail string
	d.QueryRow("SELECT result, detail FROM tiktok_live_checks ORDER BY id DESC LIMIT 1").Scan(&result, &detail)
	if result != "error" || !strings.Contains(detail, "isn't the JSON") {
		t.Errorf("last check = (%q, %q), want an error", result, detail)
	}
}

func TestTikTokLiveCheckAllDay(t *testing.T) {
	d, q, m := openDB(t)
	tt := &fakeTikTokLive{t: t, status: 4, title: "  Budget\nQ&A "}
	srv := httptest.NewServer(tt)
	defer srv.Close()
	s := media.NewTestService(q, m, nil, nil, nil)
	s.UseTikTokLiveServer(srv.URL)
	ctx := context.Background()
	at := func(hour, minute int) time.Time { return time.Date(2026, 10, 31, hour, minute, 0, 0, time.UTC) }
	check := func(now time.Time) error { return media.CheckTikTokLive(s, ctx, now) }

	// Outside 8 am to 7 pm, and between the 5 minutes, nothing is asked.
	for _, now := range []time.Time{at(7, 55), at(19, 0), at(10, 3)} {
		check(now)
	}
	if n := tt.count(); n != 0 {
		t.Errorf("%d checks outside the all-day times, want 0", n)
	}
	check(at(10, 0))
	if n := tt.count(); n != 1 {
		t.Errorf("%d checks at 10:00, want 1", n)
	}

	// Rob goes live without booking: a stream is added, already announced
	// so nobody is emailed about it.
	tt.set(2, "", 0)
	if err := check(at(10, 5)); err != nil {
		t.Fatal(err)
	}
	var title, link string
	var announced int
	d.QueryRow("SELECT title, url, announced FROM live_events WHERE id = 1").Scan(&title, &link, &announced)
	if got := liveStatuses(t, d); got != "live" || title != "Budget Q&A" || link != "https://www.tiktok.com/@rob_goudie/live" || announced != 1 {
		t.Errorf("added stream: %s %q %q announced=%d", got, title, link, announced)
	}

	// While it's on, it's checked every minute, and ends after five checks
	// off air.
	tt.set(4, "", 0)
	for minute := 6; minute <= 10; minute++ {
		check(at(10, minute))
	}
	if got := liveStatuses(t, d); got != "ended" {
		t.Errorf("after five checks off air: %s", got)
	}
	if n := tt.count(); n != 7 {
		t.Errorf("%d checks, want 7", n)
	}

	// TikTok refuses a check: the all-day checks stop for the day.
	tt.set(4, "", http.StatusForbidden)
	if err := check(at(10, 15)); err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("a refused check: %v", err)
	}
	check(at(10, 20))
	if n := tt.count(); n != 8 {
		t.Errorf("%d checks after TikTok refused one, want 8", n)
	}
	var status int
	d.QueryRow("SELECT http_status FROM tiktok_live_checks ORDER BY id DESC LIMIT 1").Scan(&status)
	if status != 403 {
		t.Errorf("logged HTTP status %d, want 403", status)
	}
	if got := outbox(t, d); len(got) != 0 {
		t.Errorf("emails: %v, want none", got)
	}
}
