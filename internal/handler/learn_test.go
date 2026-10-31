package handler

import (
	"database/sql"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/go-chi/chi/v5"
)

// learnServer serves the Learn and Learn admin routes as main.go does, with
// a few videos, streams and a question. X-User picks who's signed in: 1 is
// Rob (admin), 2 is Alice, and none is a visitor.
func learnServer(t *testing.T) http.Handler {
	t.Helper()
	d := openTestDB(t)
	if _, err := d.Exec(`
		INSERT INTO users (id, email, name, email_token, is_admin) VALUES
			(1, 'rob@x.com', 'Rob', 't1', 1), (2, 'alice@x.com', 'Alice', 't2', 0);
		INSERT INTO videos (id, platform, external_id, url, embed_url, title, description, thumb_url, published_at, duration_seconds, vertical, featured) VALUES
			(1, 'youtube', 'a', 'https://www.youtube.com/watch?v=a', 'https://www.youtube-nocookie.com/embed/a?rel=0', 'Salary sacrifice explained', 'Line one.

Line two.', 'https://i.ytimg.com/vi/a/mqdefault.jpg', '2026-10-01 00:00:00', 725, 0, 1),
			(2, 'tiktok', '7301', 'https://www.tiktok.com/@rob_goudie/video/7301', 'https://www.tiktok.com/player/v1/7301', 'Super tip', '', '', '2026-10-02 00:00:00', 40, 1, 0),
			(3, 'youtube', 'c', 'https://www.youtube.com/watch?v=c', 'https://www.youtube-nocookie.com/embed/c?rel=0', 'Retirement income', '', '', '2026-09-01 00:00:00', 900, 0, 0);
		INSERT INTO video_topics (video_id, topic_id) SELECT v.id, t.id FROM videos v, topics t WHERE t.slug = 'super';
		INSERT INTO live_events (id, platform, url, embed_url, title, scheduled_at, status, source) VALUES
			(1, 'youtube', 'https://www.youtube.com/watch?v=up', 'https://www.youtube-nocookie.com/embed/up?rel=0', 'Thursday Q&A', datetime('now', '+1 day'), 'upcoming', 'youtube'),
			(2, 'tiktok', 'https://www.tiktok.com/@rob_goudie/live', '', 'TikTok live', datetime('now', '-10 minutes'), 'live', 'admin');
		INSERT INTO live_events (id, platform, url, embed_url, video_id, title, started_at, status, source) VALUES
			(3, 'youtube', 'https://www.youtube.com/watch?v=c', 'https://www.youtube-nocookie.com/embed/c?rel=0', 3, 'Last week', '2026-09-01 00:00:00', 'ended', 'youtube');
		INSERT INTO questions (id, user_id, title, topic_id) SELECT 1, 2, 'Is super worth it?', id FROM topics WHERE slug = 'super';
		INSERT INTO tiktok_live_checks (account, booked, result, http_status, detail, event_id) VALUES
			('rob_goudie', 1, 'live', 200, 'Started the booked stream.', 2),
			('rob_goudie', 0, 'error', 403, 'TikTok answered 403 Forbidden', NULL);
	`); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	m, err := mail.New(q, mail.Log{}, mail.Config{BaseURL: "https://game.example"})
	if err != nil {
		t.Fatal(err)
	}
	h.SetMailer(m)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-User") == "" {
				next.ServeHTTP(w, r)
				return
			}
			id, _ := strconv.ParseInt(r.Header.Get("X-User"), 10, 64)
			user, err := q.GetUser(r.Context(), id)
			if err != nil {
				t.Fatalf("X-User %q: %v", r.Header.Get("X-User"), err)
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), &user)))
		})
	})
	r.Get("/learn", h.LearnHome)
	r.Get("/learn/topics/{slug}", h.LearnTopic)
	r.Get("/learn/v/{vid}", h.LearnVideo)
	r.Get("/learn/thumb/{vid}", h.LearnThumb)
	r.Get("/learn/live", h.LearnLive)
	r.Get("/learn/live/{eid}", h.LearnLiveEvent)
	r.Get("/learn/live/{eid}/queue", h.LearnLiveQueue)
	r.Get("/learn/live/{eid}/calendar.ics", h.LearnLiveCalendar)
	r.Post("/learn/live/{eid}/remind", h.LearnLiveRemind)
	r.Get("/learn/questions", h.QuestionsPage)
	r.Get("/learn/questions/{qid}", h.QuestionPage)
	r.Get("/privacy", h.PrivacyPage)
	r.Get("/sitemap.xml", h.Sitemap)
	r.Get("/robots.txt", h.Robots)
	r.Route("/admin", func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Get("/learn", h.AdminLearn)
		r.Get("/learn/videos/{vid}", h.AdminLearnVideo)
		r.Post("/learn/videos/{vid}", h.AdminLearnVideoUpdate)
		r.Get("/learn/topics", h.AdminLearnTopics)
		r.Post("/learn/topics", h.AdminLearnTopicCreate)
		r.Get("/learn/live", h.AdminLearnLive)
		r.Post("/learn/live", h.AdminLearnLiveCreate)
		r.Post("/learn/live/{eid}/{action}", h.AdminLearnLiveAction)
	})
	return r
}

func TestLearnPagesRender(t *testing.T) {
	srv := learnServer(t)
	for _, tc := range []struct {
		user, path string
		want       []string
	}{
		{"", "/learn", []string{"Learn with Rob", "Salary sacrifice explained", "Super tip", "Superannuation", "LIVE", "general in nature only", "Sign in"}},
		{"", "/learn?q=retirement", []string{"Retirement income"}},
		{"2", "/learn", []string{"Rob is live"}},
		{"", "/learn/topics/super", []string{"Superannuation", "3 videos", "Is super worth it?"}},
		{"", "/learn/v/1", []string{`data-play="https://www.youtube-nocookie.com/embed/a?rel=0"`, "12:05", "Ask Rob about this", "Retirement income"}},
		{"", "/learn/v/1?t=65", []string{"embed/a?rel=0&amp;start=65"}},
		{"", "/learn/live", []string{"Thursday Q&amp;A", "Add to calendar", "Last week"}},
		{"2", "/learn/live", []string{"Remind me"}},
		{"", "/learn/live/1", []string{"Thursday Q&amp;A", "Is super worth it?", `href="/login?next=%2Flearn%2Flive%2F1"`}},
		{"1", "/learn/live/2", []string{"End stream", "Answered", `hx-trigger="every 20s"`}},
		{"", "/learn/live/3", []string{"Recorded"}},
		{"", "/learn/questions?topic=super", []string{"Is super worth it?", "Superannuation"}},
		{"1", "/learn/questions/1", []string{"Answer with a video", "Salary sacrifice explained", "Answered on air in"}},
		{"2", "/privacy", []string{"Rob is live"}},
		{"1", "/admin/learn", []string{"YouTube is off", "Salary sacrifice explained"}},
		{"1", "/admin/learn/videos/1", []string{"Superannuation", "Pin to"}},
		{"1", "/admin/learn/topics", []string{"Superannuation", "Add a topic"}},
		{"1", "/admin/learn/live", []string{"TikTok live", "live now", `data-prefill-by="platform"`, "consortiumprivatewealth/live/", "TikTok live checks", "Today: 2 checks, 1 found Rob live, 1 failed", "1 refused by TikTok", "error (403)", `Started the booked stream. <a href="/learn/live/2">Stream</a>`, "@rob_goudie (booked)"}},
	} {
		w := as(srv, tc.user, "GET", tc.path, nil, false)
		if w.Code != http.StatusOK {
			t.Errorf("%s as %q: %d\n%s", tc.path, tc.user, w.Code, w.Body)
			continue
		}
		for _, s := range tc.want {
			if !strings.Contains(w.Body.String(), s) {
				t.Errorf("%s as %q: no %q", tc.path, tc.user, s)
			}
		}
	}

	if w := as(srv, "2", "GET", "/admin/learn", nil, false); w.Code != http.StatusForbidden {
		t.Errorf("a player on the admin page: %d", w.Code)
	}
	w := as(srv, "", "GET", "/learn/live/1/calendar.ics", nil, false)
	if body := w.Body.String(); !strings.Contains(body, "BEGIN:VEVENT\r\n") || !strings.Contains(body, `SUMMARY:Rob live on YouTube: Thursday Q&A`) {
		t.Errorf("calendar file:\n%s", body)
	}
	if w := as(srv, "", "GET", "/learn/thumb/2", nil, false); w.Header().Get("Content-Type") != "image/svg+xml" {
		t.Errorf("a missing cover should fall back to the placeholder, got %q", w.Header().Get("Content-Type"))
	}
}

func TestSitemapAndRobots(t *testing.T) {
	t.Setenv("BASE_URL", "https://site.example")
	srv := learnServer(t)
	w := as(srv, "", "GET", "/sitemap.xml", nil, false)
	body := w.Body.String()
	if w.Code != http.StatusOK || !strings.HasPrefix(body, "<?xml") {
		t.Fatalf("sitemap: %d\n%s", w.Code, body)
	}
	for _, s := range []string{
		"<loc>https://site.example/login</loc>",
		"<loc>https://site.example/learn/topics/super</loc>",
		"<loc>https://site.example/learn/v/1</loc><lastmod>2026-10-01</lastmod>",
		"<loc>https://site.example/learn/v/2</loc>",
	} {
		if !strings.Contains(body, s) {
			t.Errorf("sitemap has no %s\n%s", s, body)
		}
	}
	if strings.Contains(body, "/learn/questions/1") {
		t.Error("the sitemap lists an unanswered question")
	}

	w = as(srv, "", "GET", "/robots.txt", nil, false)
	if !strings.Contains(w.Body.String(), "Sitemap: https://site.example/sitemap.xml") {
		t.Errorf("robots.txt doesn't point at the sitemap:\n%s", w.Body)
	}
}

func TestStreamReminderAndAdminActions(t *testing.T) {
	srv := learnServer(t)
	w := as(srv, "2", "POST", "/learn/live/1/remind", map[string][]string{"on": {"1"}}, true)
	if !strings.Contains(w.Body.String(), "remind you") {
		t.Errorf("remind fragment:\n%s", w.Body)
	}
	if !strings.Contains(as(srv, "2", "GET", "/learn/live/1", nil, false).Body.String(), "remind you") {
		t.Error("the stream page doesn't show the reminder")
	}

	w = as(srv, "1", "POST", "/admin/learn/live", map[string][]string{
		"title": {"Going live on TikTok"}, "platform": {"tiktok"}, "url": {"https://www.tiktok.com/@rob_goudie/live"}, "now": {"1"},
	}, false)
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/learn/live/") {
		t.Fatalf("I'm live now: %d to %q", w.Code, w.Header().Get("Location"))
	}
	page := as(srv, "1", "GET", w.Header().Get("Location"), nil, false).Body.String()
	if !strings.Contains(page, "LIVE on TikTok") {
		t.Error("a stream started with I'm live now isn't live")
	}
	as(srv, "1", "POST", "/admin/learn/live/2/end", nil, false)
	if strings.Contains(as(srv, "", "GET", "/learn/live/2", nil, false).Body.String(), "LIVE on") {
		t.Error("ending a stream didn't")
	}

	as(srv, "1", "POST", "/admin/learn/videos/3", map[string][]string{"topic": {"1"}, "hidden": {"1"}}, false)
	if w := as(srv, "", "GET", "/learn/v/3", nil, false); w.Code != http.StatusNotFound {
		t.Errorf("a hidden video for a visitor: %d, want 404", w.Code)
	}
}

func TestLivePrefill(t *testing.T) {
	syd, err := time.LoadLocation("Australia/Sydney")
	if err != nil {
		t.Fatal(err)
	}
	old := displayLoc
	displayLoc = syd
	t.Cleanup(func() { displayLoc = old })

	// Friday 30 October 2026, 9 pm in Sydney (AEDT, UTC+11).
	now := time.Date(2026, 10, 30, 21, 0, 0, 0, syd)
	events := []db.LiveEvent{
		{Platform: "youtube", Source: "youtube", Title: "Synced", Url: "https://youtu.be/x"},
		// Friday 23 October, 7:30 pm in Sydney.
		{Platform: "tiktok", Source: "admin", Title: "Thursday Q&A", Url: "https://www.tiktok.com/@rob_goudie/live", Description: "Ask away",
			ScheduledAt: sql.NullString{String: "2026-10-23 08:30:00", Valid: true}},
		{Platform: "tiktok", Source: "admin", Title: "Older", Url: "https://www.tiktok.com/@rob_goudie/live"},
	}
	got := livePrefill(events, now)
	want := map[string]string{
		"url": "https://www.tiktok.com/@rob_goudie/live", "title": "Thursday Q&A", "description": "Ask away",
		"scheduled": "2026-11-06T19:30", // tonight's 7:30 pm has passed
	}
	if !maps.Equal(got["tiktok"], want) {
		t.Errorf("tiktok = %v, want %v", got["tiktok"], want)
	}
	if got["youtube"]["url"] != "https://www.youtube.com/@ConsortiumPW/live" || got["youtube"]["scheduled"] != "" {
		t.Errorf("youtube = %v, want Rob's live page and no time", got["youtube"])
	}

	for _, tc := range []struct{ last, want string }{
		{"2026-10-23 22:00", "2026-10-30 22:00"}, // later tonight
		{"2026-10-24 09:00", "2026-10-31 09:00"}, // tomorrow
		{"2026-10-22 09:00", "2026-11-05 09:00"}, // next Thursday
		{"2026-10-30 22:00", "2026-11-06 22:00"}, // booked for later tonight already
	} {
		last, _ := time.ParseInLocation("2006-01-02 15:04", tc.last, syd)
		if got := nextWeekly(last, now).Format("2006-01-02 15:04"); got != tc.want {
			t.Errorf("nextWeekly(%s) = %s, want %s", tc.last, got, tc.want)
		}
	}
}
