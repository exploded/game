package handler

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// newsletterServer seeds an admin (1, not subscribed) and two players, one
// of whom (3) opted in to News.
func newsletterServer(t *testing.T, sender string) (http.Handler, *sql.DB) {
	t.Helper()
	d := openTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token, is_admin) VALUES (1, 'admin@x.com', 'Admin', 't1', 1), (2, 'b@x.com', 'Bob', 't2', 0), (3, 'c@x.com', 'Cat', 't3', 0)`,
		`INSERT INTO email_prefs (user_id, category, enabled) VALUES (2, 'news', 0), (3, 'news', 1)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	m, err := mail.New(q, mail.Log{}, mail.Config{BaseURL: "https://game.example", NewsSender: sender})
	if err != nil {
		t.Fatal(err)
	}
	h.SetMailer(m)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		admin, _ := q.GetUser(r.Context(), 1)
		r = r.WithContext(auth.WithUser(r.Context(), &admin))
		switch r.Method {
		case "GET":
			h.AdminEmail(w, r)
		default:
			h.AdminEmailSend(w, r)
		}
	}), d
}

func sendNewsletter(srv http.Handler, action string) *httptest.ResponseRecorder {
	form := url.Values{"subject": {"Big news"}, "body": {"Courses are coming."}, "action": {action}}
	req := httptest.NewRequest("POST", "/admin/email", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func recipients(t *testing.T, d *sql.DB) []string {
	t.Helper()
	rows, err := d.Query("SELECT to_email || ' ' || subject FROM email_outbox ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestNewsletterTestThenSend(t *testing.T) {
	srv, d := newsletterServer(t, "Rob, PO Box 1, Melbourne VIC 3000")

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/admin/email", nil))
	if !strings.Contains(w.Body.String(), "Send to 1 subscribers") {
		t.Errorf("page doesn't count 1 subscriber:\n%s", w.Body)
	}

	if w := sendNewsletter(srv, "test"); !strings.Contains(w.Body.String(), "Test sent to admin@x.com") || !strings.Contains(w.Body.String(), "Courses are coming.") {
		t.Errorf("after the test, the page doesn't confirm it or lost the draft:\n%s", w.Body)
	}
	if got := recipients(t, d); len(got) != 1 || got[0] != "admin@x.com [Test] Big news" {
		t.Fatalf("after the test: %v, want one test to the admin", got)
	}
	w = sendNewsletter(srv, "send")
	if w.Code != http.StatusSeeOther {
		t.Fatalf("send: %d", w.Code)
	}
	got := recipients(t, d)
	if len(got) != 2 || got[1] != "c@x.com Big news" {
		t.Fatalf("after sending: %v, want only Cat added", got)
	}
	var text string
	d.QueryRow("SELECT text_body FROM email_outbox WHERE to_email = 'c@x.com'").Scan(&text)
	if !strings.Contains(text, "Sent by Rob, PO Box 1") || !strings.Contains(text, "Unsubscribe: https://game.example/email/unsubscribe?c=news&t=t3") {
		t.Errorf("newsletter lacks the sender or unsubscribe link:\n%s", text)
	}
	var n int
	d.QueryRow("SELECT recipients FROM email_broadcasts").Scan(&n)
	if n != 1 {
		t.Errorf("broadcast recorded %d recipients, want 1", n)
	}
}

func TestNewsletterNeedsSender(t *testing.T) {
	srv, d := newsletterServer(t, "")
	w := sendNewsletter(srv, "send")
	if !strings.Contains(w.Body.String(), "Set NEWS_SENDER") {
		t.Error("no error about NEWS_SENDER")
	}
	if got := recipients(t, d); len(got) != 0 {
		t.Errorf("queued %v without a sender", got)
	}
}
