package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/go-chi/chi/v5"
)

// loginServer serves the sign-in and unsubscribe routes as main.go does.
// Email stays in the outbox, where the tests read it.
func loginServer(t *testing.T) (http.Handler, *sql.DB, *db.Queries) {
	t.Helper()
	d := openTestDB(t)
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	m, err := mail.New(q, mail.Log{}, "https://game.example", "")
	if err != nil {
		t.Fatal(err)
	}
	h.SetMailer(m)
	r := chi.NewRouter()
	r.Get("/login", h.LoginPage)
	r.Post("/login", h.LoginSubmit)
	r.Get("/login/check", h.LoginCheck)
	r.Post("/login/code", h.LoginCode)
	r.Get("/login/verify", h.LoginVerifyPage)
	r.Post("/login/verify", h.LoginVerify)
	r.Get("/email/unsubscribe", h.UnsubscribePage)
	r.Post("/email/unsubscribe", h.Unsubscribe)
	return r, d, q
}

func post(srv http.Handler, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func cookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name && c.MaxAge >= 0 {
			return c
		}
	}
	return nil
}

var (
	codeRE  = regexp.MustCompile(`enter this code on the sign-in page: (\d{6})`)
	tokenRE = regexp.MustCompile(`/login/verify\?t=([0-9a-f]{64})`)
)

// requestLogin submits the email form and returns the login_req cookie and
// the code and token from the email.
func requestLogin(t *testing.T, srv http.Handler, d *sql.DB, email string) (*http.Cookie, string, string) {
	t.Helper()
	w := post(srv, "/login", url.Values{"email": {email}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login/check" {
		t.Fatalf("POST /login = %d to %q, want 303 to /login/check\n%s", w.Code, w.Header().Get("Location"), w.Body)
	}
	var body string
	if err := d.QueryRow("SELECT text_body FROM email_outbox ORDER BY id DESC LIMIT 1").Scan(&body); err != nil {
		t.Fatalf("no email queued: %v", err)
	}
	code, token := codeRE.FindStringSubmatch(body), tokenRE.FindStringSubmatch(body)
	if code == nil || token == nil {
		t.Fatalf("email has no code or link:\n%s", body)
	}
	return cookie(w, loginCookie), code[1], token[1]
}

func TestSignInWithCodeCreatesAccount(t *testing.T) {
	srv, d, q := loginServer(t)
	req, code, _ := requestLogin(t, srv, d, " New.Player@Example.com ")

	w := post(srv, "/login/code", url.Values{"code": {code[:3] + " " + code[3:]}}, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/welcome" {
		t.Fatalf("POST /login/code = %d to %q, want 303 to /welcome", w.Code, w.Header().Get("Location"))
	}
	if cookie(w, "session") == nil {
		t.Fatal("no session cookie")
	}
	user, err := q.GetUserByEmail(context.Background(), "new.player@example.com")
	if err != nil {
		t.Fatalf("user not created: %v", err)
	}
	if user.Name != "new.player" || len(user.EmailToken) != 32 {
		t.Errorf("user = %q with token %q", user.Name, user.EmailToken)
	}
	var welcome int
	d.QueryRow("SELECT COUNT(*) FROM email_outbox WHERE template = 'welcome'").Scan(&welcome)
	if welcome != 1 {
		t.Errorf("%d welcome emails queued, want 1", welcome)
	}

	// The code is used up.
	w = post(srv, "/login/code", url.Values{"code": {code}}, req)
	if w.Header().Get("Location") != "/login" || cookie(w, "session") != nil {
		t.Errorf("reusing the code: %d to %q", w.Code, w.Header().Get("Location"))
	}
}

func TestSignInWithLink(t *testing.T) {
	srv, d, q := loginServer(t)
	if _, err := q.CreateUser(context.Background(), db.CreateUserParams{Email: "rob@example.com", Name: "Rob", EmailToken: "t"}); err != nil {
		t.Fatal(err)
	}
	_, _, token := requestLogin(t, srv, d, "rob@example.com")

	// Opening the link (as a mail scanner would) doesn't use it up.
	for range 2 {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("GET", "/login/verify?t="+token, nil))
		if !strings.Contains(w.Body.String(), "rob@example.com") || !strings.Contains(w.Body.String(), `value="`+token+`"`) {
			t.Fatalf("verify page doesn't offer to sign in:\n%s", w.Body)
		}
	}
	w := post(srv, "/login/verify", url.Values{"t": {token}})
	if w.Header().Get("Location") != "/dashboard" || cookie(w, "session") == nil {
		t.Fatalf("POST /login/verify = %d to %q, want a session and /dashboard", w.Code, w.Header().Get("Location"))
	}
	w = post(srv, "/login/verify", url.Values{"t": {token}})
	if w.Header().Get("Location") != "/login" || cookie(w, "session") != nil {
		t.Errorf("reusing the link: %d to %q", w.Code, w.Header().Get("Location"))
	}
}

func TestWrongCodesLockTheRequest(t *testing.T) {
	srv, d, _ := loginServer(t)
	req, code, _ := requestLogin(t, srv, d, "a@example.com")
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 1; i < maxCodeAttempts; i++ {
		w := post(srv, "/login/code", url.Values{"code": {wrong}}, req)
		if !strings.Contains(w.Body.String(), "That code isn") {
			t.Fatalf("wrong code %d: no error shown", i)
		}
	}
	w := post(srv, "/login/code", url.Values{"code": {wrong}}, req)
	if w.Header().Get("Location") != "/login" {
		t.Fatalf("after %d wrong codes: %d to %q, want /login", maxCodeAttempts, w.Code, w.Header().Get("Location"))
	}
	w = post(srv, "/login/code", url.Values{"code": {code}}, req)
	if cookie(w, "session") != nil {
		t.Error("the right code still worked after the request was locked")
	}
}

func TestLoginEmailsAreLimited(t *testing.T) {
	srv, _, _ := loginServer(t)
	for range maxLoginsPerHour {
		post(srv, "/login", url.Values{"email": {"a@example.com"}})
	}
	w := post(srv, "/login", url.Values{"email": {"a@example.com"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "several sign-in emails") {
		t.Errorf("sign-in email %d: %d, want the form again with an error", maxLoginsPerHour+1, w.Code)
	}
	w = post(srv, "/login", url.Values{"email": {"not an email"}})
	if !strings.Contains(w.Body.String(), "Enter a valid email address") {
		t.Error("an invalid address wasn't rejected")
	}
}

func TestOneClickUnsubscribe(t *testing.T) {
	srv, _, q := loginServer(t)
	ctx := context.Background()
	user, err := q.CreateUser(ctx, db.CreateUserParams{Email: "a@example.com", Name: "A", EmailToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	// What Gmail sends for the List-Unsubscribe-Post header.
	w := post(srv, "/email/unsubscribe?t=tok&c=games", url.Values{"List-Unsubscribe": {"One-Click"}})
	if !strings.Contains(w.Body.String(), "unsubscribed") {
		t.Fatalf("one-click unsubscribe: %d\n%s", w.Code, w.Body)
	}
	prefs, _ := mail.Prefs(ctx, q, user.ID)
	if prefs[mail.Games] || !prefs[mail.Trades] {
		t.Errorf("prefs after unsubscribing from games = %v", prefs)
	}
	// Account email can't be unsubscribed from, and a bad token does nothing.
	for _, path := range []string{"/email/unsubscribe?t=tok&c=account", "/email/unsubscribe?t=nope&c=trades"} {
		w := post(srv, path, nil)
		if !strings.Contains(w.Body.String(), "This link doesn") {
			t.Errorf("POST %s didn't say the link is invalid", path)
		}
	}
}
