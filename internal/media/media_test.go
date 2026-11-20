package media_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/media"
)

// openDB returns a migrated database with three users and a CBA stock.
func openDB(t *testing.T) (*sql.DB, *db.Queries, *mail.Mailer) {
	t.Helper()
	d, err := handler.OpenDB(filepath.Join(t.TempDir(), "test.db"), game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token) VALUES
		(1, 'rob@x.com', 'Rob', 't1'), (2, 'alice@x.com', 'Alice', 't2'), (3, 'bob@x.com', 'Bob', 't3');
		INSERT INTO stocks (id, symbol, name, market, currency) VALUES (9, 'CBA', 'Commonwealth Bank', 'asx', 'AUD')`); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	m, err := mail.New(q, mail.Log{}, mail.Config{BaseURL: "https://game.example"})
	if err != nil {
		t.Fatal(err)
	}
	return d, q, m
}

func TestParseISODuration(t *testing.T) {
	for in, want := range map[string]int64{"PT12M5S": 725, "PT1H2M3S": 3723, "PT45S": 45, "P1DT1S": 86401, "P0D": 0, "": 0, "junk": 0} {
		if got := media.ParseISODuration(in); got != want {
			t.Errorf("ParseISODuration(%q) = %d, want %d", in, got, want)
		}
	}
}

// fakeYouTube serves the parts of the YouTube Data API the sync uses, plus
// youtube.com/shorts/ for telling Shorts apart.
type fakeYouTube struct {
	mu     sync.Mutex
	videos map[string]map[string]any // id -> videos.list item
	order  []string                  // the uploads playlist, newest first
	shorts map[string]bool
}

func (f *fakeYouTube) add(id string, item map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	item["id"] = id
	f.videos[id] = item
	f.order = append([]string{id}, f.order...)
}

func (f *fakeYouTube) remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.videos, id)
	f.order = slices.DeleteFunc(f.order, func(s string) bool { return s == id })
}

func (f *fakeYouTube) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	var out any
	switch {
	case strings.HasPrefix(r.URL.Path, "/shorts/"):
		if f.shorts[strings.TrimPrefix(r.URL.Path, "/shorts/")] {
			return
		}
		http.Redirect(w, r, "/watch", http.StatusSeeOther)
		return
	case r.URL.Path == "/channels" && q.Get("forHandle") == "ConsortiumPW":
		out = map[string]any{"items": []any{map[string]any{
			"contentDetails": map[string]any{"relatedPlaylists": map[string]any{"uploads": "UUrob"}},
		}}}
	case r.URL.Path == "/playlistItems" && q.Get("playlistId") == "UUrob":
		// Pages of 3, to exercise paging.
		start := 0
		fmt.Sscan(q.Get("pageToken"), &start)
		end := min(start+3, len(f.order))
		var items []any
		for _, id := range f.order[start:end] {
			items = append(items, map[string]any{"contentDetails": map[string]any{"videoId": id}})
		}
		page := map[string]any{"items": items}
		if end < len(f.order) {
			page["nextPageToken"] = fmt.Sprint(end)
		}
		out = page
	case r.URL.Path == "/videos":
		var items []any
		for _, id := range strings.Split(q.Get("id"), ",") {
			if v, ok := f.videos[id]; ok {
				items = append(items, v)
			}
		}
		out = map[string]any{"items": items}
	default:
		http.NotFound(w, r)
		return
	}
	json.NewEncoder(w).Encode(out)
}

func ytVideo(title, duration, published string) map[string]any {
	return map[string]any{
		"snippet": map[string]any{
			"title": title, "description": "About " + title, "publishedAt": published,
			"liveBroadcastContent": "none",
			"thumbnails":           map[string]any{"medium": map[string]any{"url": "https://i.ytimg.com/vi/x/mqdefault.jpg"}},
		},
		"contentDetails": map[string]any{"duration": duration},
		"statistics":     map[string]any{"viewCount": "42"},
	}
}

func TestYouTubeSync(t *testing.T) {
	d, q, m := openDB(t)
	fake := &fakeYouTube{videos: map[string]map[string]any{}, shorts: map[string]bool{"short1": true}}
	for i := range 10 {
		fake.add(fmt.Sprintf("old%d", i), ytVideo(fmt.Sprintf("Old video %d", i), "PT8M", "2025-01-01T00:00:00Z"))
	}
	fake.add("twomin", ytVideo("A two minute video", "PT2M", "2026-09-01T00:00:00Z"))
	fake.add("short1", ytVideo("A Short", "PT45S", "2026-09-02T00:00:00Z"))
	past := ytVideo("Last week's Q&A", "PT55M", "2026-10-20T08:00:00Z")
	past["liveStreamingDetails"] = map[string]any{"actualStartTime": "2026-10-20T08:00:00Z", "actualEndTime": "2026-10-20T08:55:00Z"}
	fake.add("past1", past)
	soon := time.Now().Add(3 * time.Hour).UTC()
	up := ytVideo("Super Q&A", "P0D", "2026-10-29T00:00:00Z")
	up["snippet"].(map[string]any)["liveBroadcastContent"] = "upcoming"
	up["liveStreamingDetails"] = map[string]any{"scheduledStartTime": soon.Format(time.RFC3339)}
	fake.add("up1", up)
	// Streams Rob set up and never ran stay "upcoming" on YouTube.
	nodate := ytVideo("Never booked", "P0D", "2026-01-08T00:00:00Z")
	nodate["snippet"].(map[string]any)["liveBroadcastContent"] = "upcoming"
	nodate["liveStreamingDetails"] = map[string]any{}
	fake.add("nodate", nodate)
	late := ytVideo("Booked, never run", "P0D", "2026-10-29T00:00:00Z")
	late["snippet"].(map[string]any)["liveBroadcastContent"] = "upcoming"
	late["liveStreamingDetails"] = map[string]any{"scheduledStartTime": soon.Format(time.RFC3339)}
	fake.add("late1", late)

	srv := httptest.NewServer(fake)
	defer srv.Close()
	yt := media.NewYouTube("key", "@ConsortiumPW")
	yt.UseTestServer(srv.URL, srv.URL+"/shorts/")
	s := media.NewTestService(q, m, yt, nil)
	ctx := context.Background()

	if err := media.SyncYouTube(s, ctx, true); err != nil {
		t.Fatal(err)
	}
	var n int
	d.QueryRow("SELECT COUNT(*) FROM videos").Scan(&n)
	if n != 16 {
		t.Fatalf("%d videos, want 16", n)
	}
	vertical := map[string]int{}
	rows, _ := d.Query("SELECT external_id, vertical FROM videos WHERE external_id IN ('twomin', 'short1')")
	for rows.Next() {
		var id string
		var v int
		rows.Scan(&id, &v)
		vertical[id] = v
	}
	rows.Close()
	if vertical["short1"] != 1 || vertical["twomin"] != 0 {
		t.Errorf("vertical = %v; want only the Short", vertical)
	}
	if v, _ := q.GetMediaState(ctx, media.StateBackfilled); v != "1" {
		t.Error("the backfill isn't marked done")
	}

	events := liveEvents(t, d)
	if events["Last week's Q&A"] != "ended" || events["Super Q&A"] != "upcoming" ||
		events["Booked, never run"] != "upcoming" || events["Never booked"] != "" {
		t.Fatalf("live events = %v", events)
	}
	var upStatus, upPublished string
	d.QueryRow("SELECT live_status, published_at FROM videos WHERE external_id = 'up1'").Scan(&upStatus, &upPublished)
	if upStatus != "upcoming" || upPublished != soon.Format(time.DateTime) {
		t.Errorf("upcoming stream stored as (%q, %q)", upStatus, upPublished)
	}
	latest, _ := q.ListLatestVideos(ctx, db.ListLatestVideosParams{Vertical: 0, Lim: 50})
	for _, v := range latest {
		if v.ExternalID == "up1" {
			t.Error("an upcoming stream is listed with the videos")
		}
	}

	// Rob goes live: the 2-minute check picks it up.
	fake.mu.Lock()
	up["snippet"].(map[string]any)["liveBroadcastContent"] = "live"
	up["liveStreamingDetails"] = map[string]any{"scheduledStartTime": soon.Format(time.RFC3339), "actualStartTime": time.Now().UTC().Format(time.RFC3339)}
	// The other booking is now more than a day past without starting.
	late["liveStreamingDetails"] = map[string]any{"scheduledStartTime": time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)}
	fake.mu.Unlock()
	if err := media.CheckYouTubeLive(s, ctx); err != nil {
		t.Fatal(err)
	}
	events = liveEvents(t, d)
	if got := events["Super Q&A"]; got != "live" {
		t.Errorf("after going live the event is %q", got)
	}
	if got, ok := events["Booked, never run"]; ok {
		t.Errorf("a stream a day overdue is still listed as %q", got)
	}

	// A video deleted from the channel goes at the next full sync.
	fake.remove("old3")
	if err := media.SyncYouTube(s, ctx, true); err != nil {
		t.Fatal(err)
	}
	d.QueryRow("SELECT COUNT(*) FROM videos WHERE external_id = 'old3'").Scan(&n)
	if n != 0 {
		t.Error("a deleted video is still stored")
	}
}

func liveEvents(t *testing.T, d *sql.DB) map[string]string {
	t.Helper()
	rows, err := d.Query("SELECT title, status FROM live_events")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var title, status string
		rows.Scan(&title, &status)
		out[title] = status
	}
	return out
}

// keywordClassifier puts anything mentioning super into Superannuation and
// says it discusses CBA.
type keywordClassifier struct {
	calls int
	aus   []media.Stock
}

func (k *keywordClassifier) Classify(_ context.Context, topics []media.Topic, aus []media.Stock, title, _ string) (media.Classification, error) {
	k.calls++
	k.aus = aus
	if len(topics) == 0 {
		return media.Classification{}, fmt.Errorf("no topics")
	}
	if strings.Contains(strings.ToLower(title), "super") {
		return media.Classification{Topics: []string{"super"}, Tickers: []string{"CBA.AX", "NOPE"}}, nil
	}
	return media.Classification{}, nil
}

func TestClassifyKeepsAdminTopics(t *testing.T) {
	d, q, m := openDB(t)
	if _, err := d.Exec(`
		INSERT INTO videos (id, platform, external_id, url, embed_url, title, published_at) VALUES
			(1, 'youtube', 'a', 'u', 'e', 'Salary sacrifice into super', '2026-10-01 00:00:00'),
			(2, 'youtube', 'b', 'u', 'e', 'Buying your first home', '2026-10-02 00:00:00');
		INSERT INTO video_topics (video_id, topic_id, source) SELECT 1, id, 'admin' FROM topics WHERE slug = 'retirement';
		INSERT INTO video_topics (video_id, topic_id, source) SELECT 2, id, 'claude' FROM topics WHERE slug = 'tax';
		INSERT INTO stocks (id, symbol, name, market, currency, listing) VALUES
			(10, 'CSLLY', 'CSL Limited (US OTC)', 'sp500', 'USD', 'aus'),
			(11, 'AAPL', 'Apple Inc.', 'sp500', 'USD', 'sp500');
		INSERT INTO video_stocks (video_id, stock_id, source) VALUES (2, 10, 'admin'), (2, 11, 'claude')`); err != nil {
		t.Fatal(err)
	}
	k := &keywordClassifier{}
	s := media.NewTestService(q, m, nil, k)
	if err := media.ClassifyPending(s, context.Background()); err != nil {
		t.Fatal(err)
	}
	topics := func(id int64) []string {
		rows, _ := q.ListVideoTopics(context.Background(), id)
		var out []string
		for _, r := range rows {
			out = append(out, r.Slug+":"+r.Source)
		}
		slices.Sort(out)
		return out
	}
	if got := topics(1); !slices.Equal(got, []string{"retirement:admin", "super:claude"}) {
		t.Errorf("video 1 topics = %v", got)
	}
	if got := topics(2); len(got) != 0 {
		t.Errorf("video 2 keeps Claude's old pick: %v", got)
	}
	stocks, _ := q.ListStockVideos(context.Background(), 9)
	if len(stocks) != 1 || stocks[0].ID != 1 {
		t.Errorf("CBA videos = %v, want video 1", stocks)
	}
	// Claude's old pick for video 2 is gone; the admin's stays.
	if linked, _ := q.ListVideoStocks(context.Background(), 2); len(linked) != 1 || linked[0].Symbol != "CSLLY" || linked[0].Source != "admin" {
		t.Errorf("video 2 stocks = %v, want only the admin's CSLLY", linked)
	}
	// The classifier was told the Aussie stocks' US codes.
	if !slices.Equal(k.aus, []media.Stock{{Symbol: "CSLLY", Name: "CSL Limited (US OTC)"}}) {
		t.Errorf("classifier given aus stocks %v", k.aus)
	}
	// Both are marked done, so the next run asks nothing.
	media.ClassifyPending(s, context.Background())
	if k.calls != 2 {
		t.Errorf("classifier called %d times, want 2", k.calls)
	}
}

// brokenClassifier fails on any title containing "bad" and sorts the rest
// into no topics.
type brokenClassifier struct{ calls map[string]int }

func (b *brokenClassifier) Classify(_ context.Context, _ []media.Topic, _ []media.Stock, title, _ string) (media.Classification, error) {
	b.calls[title]++
	if strings.Contains(title, "bad") {
		return media.Classification{}, fmt.Errorf("classifier answer %q: not JSON", "oops")
	}
	return media.Classification{}, nil
}

// TestClassifyOneBadVideo: a video the classifier fails on doesn't stop the
// others, and after 3 failed runs it's left alone.
func TestClassifyOneBadVideo(t *testing.T) {
	d, q, m := openDB(t)
	if _, err := d.Exec(`INSERT INTO videos (id, platform, external_id, url, embed_url, title, published_at) VALUES
		(1, 'youtube', 'a', 'u', 'e', 'A bad one', '2026-10-03 00:00:00'),
		(2, 'youtube', 'b', 'u', 'e', 'A good one', '2026-10-02 00:00:00')`); err != nil {
		t.Fatal(err)
	}
	b := &brokenClassifier{calls: map[string]int{}}
	s := media.NewTestService(q, m, nil, b)
	for range 5 {
		if err := media.ClassifyPending(s, context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if b.calls["A good one"] != 1 {
		t.Errorf("good video classified %d times, want 1", b.calls["A good one"])
	}
	if b.calls["A bad one"] != 3 {
		t.Errorf("bad video tried %d times, want 3", b.calls["A bad one"])
	}
	var classified sql.NullString
	d.QueryRow(`SELECT classified_at FROM videos WHERE id = 2`).Scan(&classified)
	if !classified.Valid {
		t.Error("the good video wasn't marked classified")
	}
}

func TestLiveEmails(t *testing.T) {
	d, q, m := openDB(t)
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format(time.DateTime) }
	if _, err := d.Exec(`INSERT INTO live_events (id, platform, url, title, scheduled_at, started_at, status) VALUES
		(1, 'youtube', 'https://youtu.be/x', 'Booked ahead', ?, NULL, 'upcoming'),
		(2, 'tiktok', 'https://tiktok.com/@rob_goudie/live', 'Booked late', ?, NULL, 'upcoming'),
		(3, 'tiktok', 'https://tiktok.com/@rob_goudie/live', 'On now', ?, ?, 'live'),
		(4, 'tiktok', 'https://tiktok.com/@rob_goudie/live', 'Found late', ?, ?, 'live');
		INSERT INTO live_reminders (event_id, user_id) VALUES (3, 2), (4, 2);
		INSERT INTO email_prefs (user_id, category, enabled) VALUES (3, 'live', 0)`,
		at(3*time.Hour), at(10*time.Minute), at(-time.Minute), at(-time.Minute), at(-3*time.Hour), at(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	s := media.NewTestService(q, m, nil, nil)
	media.NotifyLive(s, context.Background(), now)
	media.NotifyLive(s, context.Background(), now) // nothing twice

	got := outbox(t, d)
	want := []string{
		"alice@x.com live_scheduled",
		"alice@x.com live_starting",
		"rob@x.com live_scheduled",
	}
	if !slices.Equal(got, want) {
		t.Errorf("emails:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	var n int
	d.QueryRow("SELECT COUNT(*) FROM notifications WHERE user_id = 2 AND type = 'live'").Scan(&n)
	if n != 1 {
		t.Errorf("%d live notifications for Alice, want 1", n)
	}
}

func outbox(t *testing.T, d *sql.DB) []string {
	t.Helper()
	rows, err := d.Query("SELECT to_email, template FROM email_outbox ORDER BY to_email, template")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var to, tpl string
		rows.Scan(&to, &tpl)
		out = append(out, to+" "+tpl)
	}
	return out
}

func TestOnAirAnswersWaitForTheRecording(t *testing.T) {
	d, q, m := openDB(t)
	if _, err := d.Exec(`
		INSERT INTO videos (id, platform, external_id, url, embed_url, title, published_at) VALUES
			(5, 'youtube', 'rec', 'u', 'e', 'Recording', '2026-10-01 00:00:00');
		INSERT INTO live_events (id, platform, url, title, status, source, video_id) VALUES
			(1, 'youtube', 'u', 'YouTube stream', 'ended', 'youtube', NULL),
			(2, 'tiktok', 'u', 'TikTok stream', 'ended', 'admin', NULL);
		INSERT INTO questions (id, user_id, title, answer, answered_at, live_event_id, answer_seconds, answer_mailed) VALUES
			(1, 2, 'On YouTube', 'Answered live', datetime('now'), 1, 300, 0),
			(2, 2, 'On TikTok', 'Answered live', datetime('now'), 2, 60, 0);
		INSERT INTO question_votes (question_id, user_id) VALUES (1, 3), (2, 3)`); err != nil {
		t.Fatal(err)
	}
	s := media.NewTestService(q, m, nil, nil)
	media.MailOnAirAnswers(s, context.Background())
	if got := outbox(t, d); len(got) != 2 {
		t.Fatalf("emails before the YouTube recording: %v; want the TikTok question's 2", got)
	}

	// The recording arrives: question 1 is linked to it and mailed.
	d.Exec("UPDATE live_events SET video_id = 5 WHERE id = 1")
	media.MailOnAirAnswers(s, context.Background())
	media.MailOnAirAnswers(s, context.Background())
	if got := outbox(t, d); len(got) != 4 {
		t.Errorf("emails = %v; want 4", got)
	}
	var vid sql.NullInt64
	var secs int
	d.QueryRow("SELECT answer_video_id, answer_seconds FROM questions WHERE id = 1").Scan(&vid, &secs)
	if vid.Int64 != 5 || secs != 300 {
		t.Errorf("question 1 answer video = (%v, %d), want (5, 300)", vid, secs)
	}
}

func TestTikTokUser(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.tiktok.com/@rob_goudie/live":    "rob_goudie",
		"https://tiktok.com/@rob_goudie":             "rob_goudie",
		"https://www.tiktok.com/live":                "",
		"https://www.tiktok.com/@/live":              "",
		"https://www.youtube.com/@ConsortiumPW/live": "",
		"https://eviltiktok.com/@rob_goudie/live":    "",
	} {
		if got := media.TikTokUser(in); got != want {
			t.Errorf("TikTokUser(%q) = %q, want %q", in, got, want)
		}
	}
}
