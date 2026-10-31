package handler

import (
	"database/sql"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/go-chi/chi/v5"
)

// courseServer seeds Rob (1, admin), Alice (2) and Bob (3, Community email
// off), and serves the course routes as main.go does. X-User picks who's
// signed in.
func courseServer(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	d := openTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token, is_admin) VALUES
			(1, 'rob@x.com', 'Rob', 't1', 1), (2, 'alice@x.com', 'Alice', 't2', 0), (3, 'bob@x.com', 'Bob', 't3', 0)`,
		`INSERT INTO email_prefs (user_id, category, enabled) VALUES (3, 'community', 0)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
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
			id, _ := strconv.ParseInt(r.Header.Get("X-User"), 10, 64)
			user, err := q.GetUser(r.Context(), id)
			if err != nil {
				t.Fatalf("X-User %q: %v", r.Header.Get("X-User"), err)
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), &user)))
		})
	})
	r.Get("/courses", h.CoursesPage)
	r.Get("/courses/{slug}", h.CoursePage)
	r.Get("/courses/{slug}/{lid}", h.LessonPage)
	r.Post("/courses/{slug}/{lid}/complete", h.LessonComplete)
	r.Route("/admin", func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Get("/courses", h.AdminCourses)
		r.Post("/courses", h.AdminCourseCreate)
		r.Get("/courses/{cid}", h.AdminCourse)
		r.Post("/courses/{cid}/publish", h.AdminCoursePublish)
		r.Post("/courses/{cid}/lessons", h.AdminLessonCreate)
	})
	return r, d
}

func TestCourseLifecycle(t *testing.T) {
	srv, d := courseServer(t)

	if w := as(srv, "2", "POST", "/admin/courses", url.Values{"title": {"Nope"}}, false); w.Code != http.StatusForbidden {
		t.Fatalf("a player creating a course: %d, want 403", w.Code)
	}
	w := as(srv, "1", "POST", "/admin/courses", url.Values{"title": {"Investing 101: The Basics"}}, false)
	admin := w.Header().Get("Location")
	if admin != "/admin/courses/1" {
		t.Fatalf("create course: %d to %q", w.Code, admin)
	}

	// Publishing needs a lesson, and a lesson needs a video link we can embed.
	as(srv, "1", "POST", admin+"/publish", url.Values{"publish": {"1"}}, false)
	as(srv, "1", "POST", admin+"/lessons", url.Values{"title": {"Bad"}, "video_url": {"https://example.com/a.mp4"}}, false)
	for _, l := range []struct{ title, video string }{
		{"What is a share?", "https://youtu.be/dQw4w9WgXcQ"},
		{"Reading a chart", "https://vimeo.com/76979871"},
	} {
		as(srv, "1", "POST", admin+"/lessons", url.Values{"title": {l.title}, "video_url": {l.video}, "minutes": {"5"}}, false)
	}
	var lessons int
	d.QueryRow("SELECT COUNT(*) FROM lessons").Scan(&lessons)
	if lessons != 2 {
		t.Fatalf("%d lessons, want 2 (the bad link refused)", lessons)
	}
	if page := as(srv, "1", "GET", admin, nil, false).Body.String(); strings.Contains(page, "no value") || !strings.Contains(page, "Reading a chart") {
		t.Errorf("admin course page:\n%s", page)
	}

	// A draft is hidden from players but an admin can preview it.
	if w := as(srv, "2", "GET", "/courses/investing-101-the-basics", nil, false); w.Code != http.StatusNotFound {
		t.Errorf("a player opening a draft: %d, want 404", w.Code)
	}
	if w := as(srv, "1", "GET", "/courses/investing-101-the-basics", nil, false); w.Code != http.StatusOK {
		t.Errorf("an admin previewing a draft: %d", w.Code)
	}

	as(srv, "1", "POST", admin+"/publish", url.Values{"publish": {"1"}, "email": {"1"}}, false)
	as(srv, "1", "POST", admin+"/publish", url.Values{"publish": {"0"}}, false)
	as(srv, "1", "POST", admin+"/publish", url.Values{"publish": {"1"}, "email": {"1"}}, false)
	rows, _ := d.Query("SELECT to_email FROM email_outbox WHERE template = 'course_published' ORDER BY to_email")
	var got []string
	for rows.Next() {
		var to string
		rows.Scan(&to)
		got = append(got, to)
	}
	rows.Close()
	if strings.Join(got, ",") != "alice@x.com,rob@x.com" {
		t.Errorf("course emails went to %v, want Alice and Rob once (Bob has Community off)", got)
	}

	// Alice watches the first lesson and moves on to the second.
	list := as(srv, "2", "GET", "/courses", nil, false).Body.String()
	if !strings.Contains(list, "Investing 101: The Basics") || !strings.Contains(list, "2 lessons") || !strings.Contains(list, "10 min") {
		t.Errorf("courses page:\n%s", list)
	}
	if !strings.Contains(list, `href="/learn/courses" class="active"`) {
		t.Errorf("courses page has no Learn tabs:\n%s", list)
	}
	lesson := as(srv, "2", "GET", "/courses/investing-101-the-basics/1", nil, false).Body.String()
	if !strings.Contains(lesson, `href="/learn/questions"`) {
		t.Errorf("lesson page has no Learn tabs:\n%s", lesson)
	}
	if !strings.Contains(lesson, `src="https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0"`) {
		t.Errorf("lesson page has no player:\n%s", lesson)
	}
	w = as(srv, "2", "POST", "/courses/investing-101-the-basics/1/complete", nil, false)
	if w.Header().Get("Location") != "/courses/investing-101-the-basics/2" {
		t.Errorf("completing lesson 1 went to %q, want lesson 2", w.Header().Get("Location"))
	}
	if page := as(srv, "2", "GET", "/courses/investing-101-the-basics", nil, false).Body.String(); !strings.Contains(page, "1 of 2 lessons done") {
		t.Errorf("course page doesn't show progress:\n%s", page)
	}
}

// TestCPWCoursesImported checks the courses copied from the CPW site: all
// published, every lesson playable and numbered 1 to n, and nothing added
// twice when the migrations run again.
func TestCPWCoursesImported(t *testing.T) {
	d := openTestDB(t)
	for range 2 {
		if err := RunSeedMigrations(d); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]int{
		"how-to-invest-secrets-learnt-from-20-years-in-finance": 7,
		"how-to-pay-off-your-home-loan-faster":                  16,
		"beating-mortgage-stress":                               30,
		"investing-in-the-right-asset-class":                    8,
		"beginners-guide-to-super":                              11,
		"how-super-works":                                       11,
		"understanding-credit-cards":                            11,
	}
	rows, err := d.Query(`SELECT c.slug, c.published, c.published_at IS NOT NULL,
		l.title, l.video_url, l.embed_url, l.minutes, l.sort_order
		FROM courses c JOIN lessons l ON l.course_id = c.id ORDER BY c.sort_order, l.sort_order`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var slug, title, videoURL, embed string
		var published, minutes, order int64
		var publishedAt bool
		if err := rows.Scan(&slug, &published, &publishedAt, &title, &videoURL, &embed, &minutes, &order); err != nil {
			t.Fatal(err)
		}
		got[slug]++
		if published != 1 || !publishedAt {
			t.Errorf("%s isn't published with a published_at", slug)
		}
		if order != int64(got[slug]) {
			t.Errorf("%s: %q is lesson %d, want %d", slug, title, order, got[slug])
		}
		if e, ok := embedURL(videoURL); !ok || e != embed {
			t.Errorf("%s: %q embeds %q, embedURL(%q) gives %q", slug, title, embed, videoURL, e)
		}
		if minutes < 1 || title == "" {
			t.Errorf("%s lesson %d: title %q, %d minutes", slug, order, title, minutes)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for slug, n := range want {
		if got[slug] != n {
			t.Errorf("%s has %d lessons, want %d", slug, got[slug], n)
		}
	}
	if len(got) != len(want) {
		t.Errorf("imported courses %v, want %v", got, want)
	}
}
