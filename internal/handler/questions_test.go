package handler

import (
	"database/sql"
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
	"github.com/go-chi/chi/v5"
)

// questionServer seeds Rob (1, admin), Alice (2) and Bob (3), and serves the
// question routes as main.go does. X-User picks who's signed in.
func questionServer(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	d := openTestDB(t)
	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token, is_admin) VALUES
		(1, 'rob@x.com', 'Rob', 't1', 1), (2, 'alice@x.com', 'Alice', 't2', 0), (3, 'bob@x.com', 'Bob', 't3', 0)`); err != nil {
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
	r.Get("/learn/questions", h.QuestionsPage)
	r.Post("/learn/questions", h.QuestionCreate)
	r.Get("/learn/questions/{qid}", h.QuestionPage)
	r.Post("/learn/questions/{qid}/vote", h.QuestionVote)
	r.Post("/learn/questions/{qid}/delete", h.QuestionDelete)
	r.With(auth.RequireAdmin).Post("/learn/questions/{qid}/answer", h.QuestionAnswer)
	r.With(auth.RequireAdmin).Post("/learn/questions/{qid}/onair", h.QuestionOnAir)
	r.With(auth.RequireAdmin).Post("/learn/questions/{qid}/hide", h.QuestionHide)
	r.Get("/learn/live/{eid}", h.LearnLiveEvent)
	r.Get("/questions", OldQuestionsRedirect)
	r.Get("/questions/{qid}", OldQuestionsRedirect)
	return r, d
}

func as(srv http.Handler, user, method, path string, form url.Values, htmx bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-User", user)
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func ask(t *testing.T, srv http.Handler, user, title string) string {
	t.Helper()
	w := as(srv, user, "POST", "/learn/questions", url.Values{"title": {title}}, false)
	loc := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/learn/questions/") {
		t.Fatalf("asking %q: %d to %q\n%s", title, w.Code, loc, w.Body)
	}
	return loc
}

func TestAskVoteAndList(t *testing.T) {
	srv, _ := questionServer(t)
	ask(t, srv, "2", "How do dividends work?")
	second := ask(t, srv, "3", "What is a stock split?")

	// Alice votes for Bob's question, so it has 2 votes to her own 1.
	w := as(srv, "2", "POST", second+"/vote", nil, true)
	if body := w.Body.String(); !strings.Contains(body, `aria-pressed="true"`) || !strings.Contains(body, "2 votes") || strings.Contains(body, "<html") {
		t.Fatalf("vote fragment:\n%s", body)
	}
	top := as(srv, "2", "GET", "/learn/questions", nil, false).Body.String()
	if strings.Index(top, "What is a stock split?") > strings.Index(top, "How do dividends work?") {
		t.Error("most votes isn't first")
	}
	newest := as(srv, "2", "GET", "/learn/questions?sort=new", nil, false).Body.String()
	if strings.Index(newest, "What is a stock split?") > strings.Index(newest, "How do dividends work?") {
		t.Error("newest isn't first")
	}

	// Voting again takes the vote back.
	w = as(srv, "2", "POST", second+"/vote", nil, true)
	if !strings.Contains(w.Body.String(), "1 votes") {
		t.Errorf("after unvoting:\n%s", w.Body)
	}
}

func TestAnswerEmailsAskerAndVotersOnce(t *testing.T) {
	srv, d := questionServer(t)
	q := ask(t, srv, "2", "How do dividends work?")
	as(srv, "3", "POST", q+"/vote", nil, true)

	if w := as(srv, "2", "POST", q+"/answer", url.Values{"answer": {"Not you."}}, false); w.Code != http.StatusForbidden {
		t.Fatalf("a player answering: %d, want 403", w.Code)
	}
	as(srv, "1", "POST", q+"/answer", url.Values{"answer": {"A company pays you part of its profit.\n\nMore at https://example.com"}}, false)
	as(srv, "1", "POST", q+"/answer", url.Values{"answer": {"Edited answer."}}, false)

	rows, _ := d.Query("SELECT to_email, subject FROM email_outbox ORDER BY to_email")
	defer rows.Close()
	var got []string
	for rows.Next() {
		var to, subject string
		rows.Scan(&to, &subject)
		got = append(got, to+": "+subject)
	}
	want := []string{
		"alice@x.com: Your question was answered: How do dividends work?",
		"bob@x.com: A question you voted for was answered: How do dividends work?",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("emails:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	page := as(srv, "3", "GET", q, nil, false).Body.String()
	if !strings.Contains(page, "Answer from Rob") || !strings.Contains(page, "Edited answer.") {
		t.Errorf("question page doesn't show the edited answer")
	}
}

func TestDeleteAndHide(t *testing.T) {
	srv, _ := questionServer(t)
	q := ask(t, srv, "2", "Delete me?")
	as(srv, "3", "POST", q+"/delete", nil, false)
	if w := as(srv, "3", "GET", q, nil, false); w.Code != http.StatusOK {
		t.Fatal("Bob deleted Alice's question")
	}
	as(srv, "2", "POST", q+"/delete", nil, false)
	if w := as(srv, "2", "GET", q, nil, false); w.Code != http.StatusNotFound {
		t.Errorf("after Alice deleted it: %d, want 404", w.Code)
	}

	q = ask(t, srv, "2", "Spam?")
	if w := as(srv, "2", "POST", q+"/hide", nil, false); w.Code != http.StatusForbidden {
		t.Errorf("a player hiding: %d, want 403", w.Code)
	}
	as(srv, "1", "POST", q+"/hide", nil, false)
	if strings.Contains(as(srv, "2", "GET", "/learn/questions", nil, false).Body.String(), "Spam?") {
		t.Error("a hidden question is still listed")
	}
}

func TestVisitorsCanReadQuestions(t *testing.T) {
	srv, _ := questionServer(t)
	q := ask(t, srv, "2", "Is super worth it?")

	list := as(srv, "", "GET", "/learn/questions", nil, false)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "Is super worth it?") {
		t.Fatalf("visitor list: %d\n%s", list.Code, list.Body)
	}
	if body := list.Body.String(); !strings.Contains(body, "Sign in to ask") || strings.Contains(body, `action="/learn/questions/1/vote"`) {
		t.Error("a visitor should get sign-in links, not the ask and vote forms")
	}
	if w := as(srv, "", "GET", q, nil, false); w.Code != http.StatusOK {
		t.Errorf("visitor question page: %d", w.Code)
	}

	w := as(srv, "", "GET", "/questions/1?x=1", nil, false)
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/learn/questions/1?x=1" {
		t.Errorf("old link: %d to %q", w.Code, w.Header().Get("Location"))
	}
}

func TestAnswerWithVideo(t *testing.T) {
	srv, d := questionServer(t)
	if _, err := d.Exec(`INSERT INTO videos (id, platform, external_id, url, embed_url, title, published_at)
		VALUES (7, 'youtube', 'abc', 'https://www.youtube.com/watch?v=abc', 'https://www.youtube-nocookie.com/embed/abc?rel=0', 'Super explained', '2026-10-01 00:00:00')`); err != nil {
		t.Fatal(err)
	}
	q := ask(t, srv, "2", "How does super work?")
	as(srv, "1", "POST", q+"/answer", url.Values{"video_id": {"7"}, "at": {"1:30"}}, false)

	page := as(srv, "2", "GET", q, nil, false).Body.String()
	if !strings.Contains(page, "embed/abc?rel=0&amp;start=90") || !strings.Contains(page, "from 1:30") {
		t.Errorf("question page doesn't play the video from 1:30:\n%s", page)
	}
	var n int
	d.QueryRow("SELECT COUNT(*) FROM email_outbox WHERE subject LIKE 'Your question was answered%'").Scan(&n)
	if n != 1 {
		t.Errorf("%d answer emails, want 1", n)
	}
}

func TestAnsweredOnAirWaitsForTheStream(t *testing.T) {
	srv, d := questionServer(t)
	if _, err := d.Exec(`INSERT INTO live_events (id, platform, url, title, status, source, started_at)
		VALUES (4, 'tiktok', 'https://www.tiktok.com/@rob_goudie/live', 'Super Q&A', 'live', 'admin', datetime('now', '-10 minutes'))`); err != nil {
		t.Fatal(err)
	}
	q := ask(t, srv, "2", "Can I add to super after 75?")
	as(srv, "3", "POST", q+"/vote", nil, true)

	w := as(srv, "1", "POST", q+"/onair", url.Values{"event_id": {"4"}}, true)
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), "Can I add to super after 75?") {
		t.Fatalf("the queue should drop the answered question: %d\n%s", w.Code, w.Body)
	}
	var mailed, seconds int
	d.QueryRow("SELECT answer_mailed, answer_seconds FROM questions").Scan(&mailed, &seconds)
	if mailed != 0 || seconds < 500 || seconds > 560 {
		t.Errorf("answer_mailed = %d, answer_seconds = %d; want 0 and about 9 minutes", mailed, seconds)
	}
	var n int
	d.QueryRow("SELECT COUNT(*) FROM email_outbox").Scan(&n)
	if n != 0 {
		t.Errorf("%d emails went before the stream ended", n)
	}
	page := as(srv, "", "GET", "/learn/live/4", nil, false).Body.String()
	if !strings.Contains(page, "Answered on air") {
		t.Error("the stream page doesn't list the answered question")
	}
}
