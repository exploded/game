package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/vimeo"
	"github.com/go-chi/chi/v5"
)

// courseServer seeds Rob (1, admin), Alice (2) and Bob (3, Community email
// off), and serves the course routes as main.go does. X-User picks who's
// signed in.
func courseServer(t *testing.T, setup ...func(*Handler)) (http.Handler, *sql.DB) {
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
	for _, f := range setup {
		f(h)
	}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-User") == "" {
				next.ServeHTTP(w, r) // a visitor
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
	r.Get("/learn/courses", h.CoursesPage)
	r.Get("/learn/courses/{slug}", h.CoursePage)
	r.Get("/learn/courses/{slug}/{lid}", h.LessonPage)
	r.Get("/learn/lesson-thumb/{lid}", h.LessonThumb)
	r.Post("/learn/courses/{slug}/{lid}/complete", h.LessonComplete)
	r.Get("/courses", h.OldCourseURL)
	r.Get("/courses/*", h.OldCourseURL)
	r.Route("/admin", func(r chi.Router) {
		r.Use(auth.RequireAdmin)
		r.Get("/courses", h.AdminCourses)
		r.Post("/courses", h.AdminCourseCreate)
		r.Get("/courses/{cid}", h.AdminCourse)
		r.Post("/courses/{cid}/publish", h.AdminCoursePublish)
		r.Post("/courses/{cid}", h.AdminCourseUpdate)
		r.Post("/courses/{cid}/lessons", h.AdminLessonCreate)
		r.Post("/courses/{cid}/vimeo", h.AdminCourseVimeo)
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
	if w := as(srv, "2", "GET", "/learn/courses/investing-101-the-basics", nil, false); w.Code != http.StatusNotFound {
		t.Errorf("a player opening a draft: %d, want 404", w.Code)
	}
	if w := as(srv, "1", "GET", "/learn/courses/investing-101-the-basics", nil, false); w.Code != http.StatusOK {
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
	list := as(srv, "2", "GET", "/learn/courses", nil, false).Body.String()
	if !strings.Contains(list, "Investing 101: The Basics") || !strings.Contains(list, "2 lessons") || !strings.Contains(list, "10 min") {
		t.Errorf("courses page:\n%s", list)
	}
	if !strings.Contains(list, `href="/learn/courses" aria-current="page"`) {
		t.Errorf("courses page has no Learn tabs:\n%s", list)
	}
	lesson := as(srv, "2", "GET", "/learn/courses/investing-101-the-basics/1", nil, false).Body.String()
	if !strings.Contains(lesson, `href="/learn/questions"`) {
		t.Errorf("lesson page has no Learn tabs:\n%s", lesson)
	}
	if !strings.Contains(lesson, `data-play="https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?rel=0"`) {
		t.Errorf("lesson page has no player:\n%s", lesson)
	}
	w = as(srv, "2", "POST", "/learn/courses/investing-101-the-basics/1/complete", nil, false)
	if w.Header().Get("Location") != "/learn/courses/investing-101-the-basics/2" {
		t.Errorf("completing lesson 1 went to %q, want lesson 2", w.Header().Get("Location"))
	}
	if page := as(srv, "2", "GET", "/learn/courses/investing-101-the-basics", nil, false).Body.String(); !strings.Contains(page, "1 of 2 done") || !strings.Contains(page, "Carry on: lesson 2") {
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

// TestCoursesArePublic checks a visitor can browse a published course and
// its lessons but gets a sign-in button instead of the player, and that the
// old /courses addresses move to /learn/courses.
func TestCoursesArePublic(t *testing.T) {
	srv, _ := courseServer(t)
	as(srv, "1", "POST", "/admin/courses", url.Values{"title": {"Shares 101"}}, false)
	as(srv, "1", "POST", "/admin/courses/1", url.Values{"title": {"Shares 101"}, "art": {"market"},
		"summary": {"Start here.\n\nIn this course, you'll learn about:\n\n☑️ What a share is\n☑️How prices move"}}, false)
	as(srv, "1", "POST", "/admin/courses/1/lessons", url.Values{"title": {"What is a share?"}, "video_url": {"https://vimeo.com/76979871/abc123"}, "minutes": {"4"}}, false)
	if w := as(srv, "", "GET", "/learn/courses/shares-101", nil, false); w.Code != http.StatusNotFound {
		t.Errorf("a visitor opening a draft: %d, want 404", w.Code)
	}
	as(srv, "1", "POST", "/admin/courses/1/publish", url.Values{"publish": {"1"}}, false)

	list := as(srv, "", "GET", "/learn/courses", nil, false).Body.String()
	for _, want := range []string{"Shares 101", "course-tile-market", "1 lesson", "Sign in free"} {
		if !strings.Contains(list, want) {
			t.Errorf("courses page for a visitor: no %q", want)
		}
	}
	course := as(srv, "", "GET", "/learn/courses/shares-101", nil, false).Body.String()
	for _, want := range []string{"Start here.", "What you'll learn", "<li>What a share is</li>", "<li>How prices move</li>", "Sign in free to watch", "What is a share?"} {
		if !strings.Contains(course, want) {
			t.Errorf("course page for a visitor: no %q", want)
		}
	}
	if strings.Contains(course, "learn about:") {
		t.Error("course page repeats the list's lead-in line")
	}
	lesson := as(srv, "", "GET", "/learn/courses/shares-101/1", nil, false).Body.String()
	if strings.Contains(lesson, "data-play") || strings.Contains(lesson, "player.vimeo.com") || !strings.Contains(lesson, "Sign in to watch") {
		t.Errorf("lesson page for a visitor shows the player or no sign-in:\n%s", lesson)
	}
	if lesson := as(srv, "2", "GET", "/learn/courses/shares-101/1", nil, false).Body.String(); !strings.Contains(lesson, `data-play="https://player.vimeo.com/video/76979871?h=abc123"`) {
		t.Errorf("lesson page for a player has no player")
	}

	for old, want := range map[string]string{
		"/courses":              "/learn/courses",
		"/courses/shares-101/1": "/learn/courses/shares-101/1",
	} {
		w := as(srv, "", "GET", old, nil, false)
		if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != want {
			t.Errorf("%s: %d to %q, want 301 to %q", old, w.Code, w.Header().Get("Location"), want)
		}
	}
}

func TestCourseOutline(t *testing.T) {
	o := courseOutline("First para.\n\nSecond para.\n\nIn this course, you'll learn about:\n\n☑️One\n☑️ Two\n\nBy the end, you'll know.")
	if len(o.Intro) != 2 || len(o.Learn) != 2 || o.Learn[0] != "One" || o.Learn[1] != "Two" || len(o.Outro) != 1 {
		t.Errorf("outline = %+v", o)
	}
	// No list: the whole summary is the intro.
	if o := courseOutline("Just this:\n\nAnd this."); len(o.Intro) != 2 || o.Learn != nil {
		t.Errorf("plain outline = %+v", o)
	}
}

// TestFillCourseFromVimeo imports a Vimeo folder's videos as lessons with
// thumbnails, and importing again adds nothing.
func TestFillCourseFromVimeo(t *testing.T) {
	jpeg := []byte("\xff\xd8\xff\xe0\x00\x10JFIF\x00")
	// Lesson 2 has no thumbnail on Vimeo until the second import; lesson 3
	// never has one.
	imports := 0
	var api *httptest.Server
	api = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/8765158/projects/26617132/items":
			video := func(id, name string, secs int, picType string) map[string]any {
				return map[string]any{"type": "video", "video": map[string]any{
					"uri": "/videos/" + id, "name": name, "duration": secs, "created_time": "2026-02-06T01:00:00+00:00",
					"player_embed_url": "https://player.vimeo.com/video/" + id + "?h=ff" + id,
					"pictures":         map[string]any{"type": picType, "sizes": []any{map[string]any{"width": 640, "link": api.URL + "/thumb/" + id}}},
					"privacy":          map[string]string{"view": "unlisted", "embed": "public"},
				}}
			}
			second := "default"
			if imports > 1 {
				second = "custom"
			}
			json.NewEncoder(w).Encode(map[string]any{"data": []any{
				video("222", "Lesson 2: How prices move", 250, second),
				video("333", "Lesson 3: Wrap up", 30, "default"),
				video("111", "Lesson 1:  What a share is", 61, "custom"),
			}})
		case "/thumb/111", "/thumb/222":
			w.Write(jpeg)
		default:
			http.NotFound(w, r)
		}
	}))
	defer api.Close()
	srv, d := courseServer(t, func(h *Handler) {
		h.SetVimeo(vimeo.New("tok").WithBase(api.URL).WithHTTP(api.Client()))
	})
	as(srv, "1", "POST", "/admin/courses", url.Values{"title": {"Shares 101"}}, false)
	folder := "https://vimeo.com/user/8765158/folder/26617132"

	page := as(srv, "1", "GET", "/admin/courses/1?vimeo="+url.QueryEscape(folder), nil, false).Body.String()
	if !strings.Contains(page, "<li>What a share is") || !strings.Contains(page, "Add these as lessons") {
		t.Errorf("admin page doesn't preview the folder:\n%s", page)
	}
	for range 2 {
		imports++
		as(srv, "1", "POST", "/admin/courses/1/vimeo", url.Values{"folder": {folder}}, false)
	}
	rows, err := d.Query("SELECT l.title, l.embed_url, l.minutes, COALESCE(t.content_type, '') FROM lessons l LEFT JOIN lesson_thumbs t ON t.lesson_id = l.id ORDER BY l.sort_order")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var title, embed, ct string
		var minutes int
		if err := rows.Scan(&title, &embed, &minutes, &ct); err != nil {
			t.Fatal(err)
		}
		got = append(got, title+"|"+embed+"|"+strconv.Itoa(minutes)+"|"+ct)
	}
	want := []string{
		"What a share is|https://player.vimeo.com/video/111?h=ff111|2|image/jpeg",
		"How prices move|https://player.vimeo.com/video/222?h=ff222|5|image/jpeg",
		"Wrap up|https://player.vimeo.com/video/333?h=ff333|1|",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("lessons:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	w := as(srv, "", "GET", "/learn/lesson-thumb/1", nil, false)
	if w.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("lesson thumb: %q", w.Header().Get("Content-Type"))
	}

	// The admin page remembers the folder for next time.
	page = as(srv, "1", "GET", "/admin/courses/1", nil, false).Body.String()
	if !strings.Contains(page, `value="`+folder+`"`) {
		t.Errorf("folder not remembered:\n%s", page)
	}

	// Until the course is set to use Vimeo's thumbnails, every lesson shows
	// the course's picture.
	as(srv, "1", "POST", "/admin/courses/1/publish", url.Values{"publish": {"1"}}, false)
	page = as(srv, "1", "GET", "/learn/courses/shares-101", nil, false).Body.String()
	if n := strings.Count(page, `<img class="lesson-thumb"`); n != 0 {
		t.Errorf("lesson thumbnails before the switch = %d, want 0", n)
	}
	if n := strings.Count(page, `<span class="lesson-thumb course-tile-generic">`); n != 3 {
		t.Errorf("course pictures before the switch = %d, want 3", n)
	}

	// With it on, lesson 3 (no thumbnail on Vimeo) still gets the picture.
	as(srv, "1", "POST", "/admin/courses/1", url.Values{"title": {"Shares 101"}, "vimeo_thumbs": {"1"}}, false)
	page = as(srv, "1", "GET", "/learn/courses/shares-101", nil, false).Body.String()
	if n := strings.Count(page, `<img class="lesson-thumb"`); n != 2 {
		t.Errorf("lesson thumbnails = %d, want 2", n)
	}
	if !strings.Contains(page, `<span class="lesson-thumb course-tile-generic">`) {
		t.Errorf("no course picture for the lesson without a thumbnail:\n%s", page)
	}

	// A share link can't be read: the page says how to get the right one.
	page = as(srv, "1", "GET", "/admin/courses/1?vimeo="+url.QueryEscape("https://vimeo.com/share/folder/2f489ff7"), nil, false).Body.String()
	if !strings.Contains(page, "isn&#39;t a Vimeo folder link") {
		t.Errorf("share link not explained:\n%s", page)
	}
}

// TestCourses2026Imported checks Rob's 2026 courses and his investment
// presentation arrive as drafts, each with a cover, a "what you'll learn"
// list and its Vimeo folder, once.
func TestCourses2026Imported(t *testing.T) {
	d := openTestDB(t)
	for range 2 {
		if err := RunSeedMigrations(d); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := d.Query(`SELECT slug, summary, art, published, vimeo_folder FROM courses WHERE art != '' ORDER BY sort_order`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var slug, summary, art, folder string
		var published int64
		if err := rows.Scan(&slug, &summary, &art, &published, &folder); err != nil {
			t.Fatal(err)
		}
		n++
		if _, _, ok := vimeo.ParseFolderURL(folder); !ok {
			t.Errorf("%s: Vimeo folder %q", slug, folder)
		}
		o := courseOutline(summary)
		if published != 0 || !validArt(art) || len(o.Intro) == 0 || len(o.Learn) < 5 || strings.Contains(summary, "General advice only") {
			t.Errorf("%s: published %d, art %q, %d intro paragraphs, %d list items", slug, published, art, len(o.Intro), len(o.Learn))
		}
	}
	if n != 11 {
		t.Errorf("%d courses with covers, want 11", n)
	}
}
