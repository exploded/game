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
			id, _ := strconv.ParseInt(r.Header.Get("X-User"), 10, 64)
			user, err := q.GetUser(r.Context(), id)
			if err != nil {
				t.Fatalf("X-User %q: %v", r.Header.Get("X-User"), err)
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), &user)))
		})
	})
	r.Get("/questions", h.QuestionsPage)
	r.Post("/questions", h.QuestionCreate)
	r.Get("/questions/{qid}", h.QuestionPage)
	r.Post("/questions/{qid}/vote", h.QuestionVote)
	r.Post("/questions/{qid}/delete", h.QuestionDelete)
	r.With(auth.RequireAdmin).Post("/questions/{qid}/answer", h.QuestionAnswer)
	r.With(auth.RequireAdmin).Post("/questions/{qid}/hide", h.QuestionHide)
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
	w := as(srv, user, "POST", "/questions", url.Values{"title": {title}}, false)
	loc := w.Header().Get("Location")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/questions/") {
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
	top := as(srv, "2", "GET", "/questions", nil, false).Body.String()
	if strings.Index(top, "What is a stock split?") > strings.Index(top, "How do dividends work?") {
		t.Error("most votes isn't first")
	}
	newest := as(srv, "2", "GET", "/questions?sort=new", nil, false).Body.String()
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
	if strings.Contains(as(srv, "2", "GET", "/questions", nil, false).Body.String(), "Spam?") {
		t.Error("a hidden question is still listed")
	}
}
