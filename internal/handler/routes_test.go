package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/go-chi/chi/v5"
)

var siteSecret = []byte("test csrf secret")

// siteServer is the whole site as cmd/server serves it (Routes), with Rob
// (1, admin), Alice (2) and Bob (3) signed in as sessions "s1", "s2", "s3",
// and an active game (1) that Alice and Bob play.
func siteServer(t *testing.T) (*chi.Mux, *sql.DB) {
	t.Helper()
	d := openTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token, is_admin) VALUES
			(1, 'rob@x.com', 'Rob', 't1', 1), (2, 'alice@x.com', 'Alice', 't2', 0), (3, 'bob@x.com', 'Bob', 't3', 0)`,
		`INSERT INTO games (id, created_by, name, status, start_date, end_date) VALUES (1, 1, 'Cup', 'active', '2026-01-01', '2099-12-31')`,
		`INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES
			(1, 1, 2, 100000000, 100000000), (2, 1, 3, 100000000, 100000000)`,
		`INSERT INTO stocks (id, symbol, name, market) VALUES (1, 'AAPL', 'Apple', 'sp500')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	for id := int64(1); id <= 3; id++ {
		if _, err := d.Exec("INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, datetime('now', '+1 day'))",
			auth.SessionKey(session(id)), id); err != nil {
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
	static, err := NewStatic(game.Static())
	if err != nil {
		t.Fatal(err)
	}
	r, ok := h.Routes(static, siteSecret, NewThrottle(8, 64, 3*time.Second, 50*time.Second)).(*chi.Mux)
	if !ok {
		t.Fatal("Routes isn't a chi router")
	}
	return r, d
}

// session is user id's session cookie in siteServer.
func session(id int64) string { return "s" + string(rune('0'+id)) }

// csrfFor is the CSRF token CSRFProtect expects with a session cookie.
func csrfFor(sessionID string) string {
	mac := hmac.New(sha256.New, siteSecret)
	mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

// siteRequest sends method path as user (0 for a visitor), with the form
// and, when withToken, the user's CSRF token in the header.
func siteRequest(srv http.Handler, user int64, method, path string, form url.Values, withToken bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if user != 0 {
		req.AddCookie(&http.Cookie{Name: "session", Value: session(user)})
		if withToken {
			req.Header.Set("X-CSRF-Token", csrfFor(session(user)))
		}
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

var routeParam = regexp.MustCompile(`\{[^}]+\}`)

// Every admin route refuses a player and sends a visitor to sign in. The
// routes come from the router itself, so a new admin route is checked
// without anyone adding it here.
func TestAdminRoutesRefuseNonAdmins(t *testing.T) {
	srv, _ := siteServer(t)
	type route struct{ method, path string }
	var admin []route
	err := chi.Walk(srv, func(method, pattern string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if strings.HasPrefix(pattern, "/admin") {
			path := routeParam.ReplaceAllString(strings.TrimSuffix(pattern, "/*"), "1")
			admin = append(admin, route{method, path})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(admin) < 40 {
		t.Fatalf("found only %d admin routes", len(admin))
	}
	// Admin-only actions outside /admin.
	for _, p := range []string{"answer", "onair", "hide"} {
		admin = append(admin, route{"POST", "/learn/questions/1/" + p})
	}
	for _, rt := range admin {
		if w := siteRequest(srv, 2, rt.method, rt.path, nil, true); w.Code != http.StatusForbidden {
			t.Errorf("%s %s as a player: %d, want 403", rt.method, rt.path, w.Code)
		}
		w := siteRequest(srv, 0, rt.method, rt.path, nil, false)
		if w.Code != http.StatusTemporaryRedirect || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
			t.Errorf("%s %s as a visitor: %d to %q, want a redirect to sign in", rt.method, rt.path, w.Code, w.Header().Get("Location"))
		}
	}
	// And the admin gets in.
	if w := siteRequest(srv, 1, "GET", "/admin/", nil, false); w.Code != http.StatusOK {
		t.Errorf("GET /admin/ as the admin: %d", w.Code)
	}
}

// A signed-in POST without the CSRF token, or with someone else's, is
// refused before the handler runs.
func TestSignedInPostNeedsCSRFToken(t *testing.T) {
	srv, d := siteServer(t)
	form := url.Values{"confirm_delete": {"DELETE"}}
	if w := siteRequest(srv, 2, "POST", "/settings/delete", form, false); w.Code != http.StatusForbidden {
		t.Errorf("no token: %d, want 403", w.Code)
	}
	form.Set("_csrf", csrfFor(session(3)))
	if w := siteRequest(srv, 2, "POST", "/settings/delete", form, false); w.Code != http.StatusForbidden {
		t.Errorf("Bob's token: %d, want 403", w.Code)
	}
	var deleted sql.NullString
	d.QueryRow("SELECT deleted_at FROM users WHERE id = 2").Scan(&deleted)
	if deleted.Valid {
		t.Fatal("Alice's account was deleted without a CSRF token")
	}
	// With her own token it works, so the 403s weren't for something else.
	if w := siteRequest(srv, 2, "POST", "/settings/delete", url.Values{"confirm_delete": {"DELETE"}}, true); w.Code != http.StatusSeeOther {
		t.Errorf("with the token: %d, want 303", w.Code)
	}
}

// Another site can't post the sign-in form, which would sign the visitor in
// to an account the attacker controls.
func TestCrossOriginSignInRefused(t *testing.T) {
	srv, d := siteServer(t)
	req := httptest.NewRequest("POST", "/login", strings.NewReader("email=victim%40example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("cross-origin POST /login: %d, want 403", w.Code)
	}
	var n int
	d.QueryRow("SELECT COUNT(*) FROM login_requests").Scan(&n)
	if n != 0 {
		t.Error("a cross-origin POST started a sign-in")
	}
	// The same post from the site itself goes through.
	req = httptest.NewRequest("POST", "/login", strings.NewReader("email=victim%40example.com"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Errorf("same-origin POST /login: %d, want 303", w.Code)
	}
}

// A player can't cancel someone else's order: it's not found, and stays open.
func TestCancelOtherPlayersOrder(t *testing.T) {
	srv, d := siteServer(t)
	if _, err := d.Exec(`INSERT INTO limit_orders (id, participant_id, stock_id, type, quantity, limit_price)
		VALUES (1, 2, 1, 'buy', 5, 10000)`); err != nil {
		t.Fatal(err)
	}
	if w := siteRequest(srv, 2, "POST", "/games/1/orders/1/cancel", nil, true); w.Code != http.StatusNotFound {
		t.Errorf("Alice cancelling Bob's order: %d, want 404", w.Code)
	}
	var status string
	d.QueryRow("SELECT status FROM limit_orders WHERE id = 1").Scan(&status)
	if status != "open" {
		t.Fatalf("Bob's order is %q after Alice's cancel, want open", status)
	}
	// Bob can.
	if w := siteRequest(srv, 3, "POST", "/games/1/orders/1/cancel", nil, true); w.Code != http.StatusSeeOther {
		t.Errorf("Bob cancelling his order: %d, want 303", w.Code)
	}
	d.QueryRow("SELECT status FROM limit_orders WHERE id = 1").Scan(&status)
	if status != "cancelled" {
		t.Errorf("Bob's order is %q after his cancel", status)
	}
}

// noisePNG is a w × h PNG of random pixels, which doesn't compress.
func noisePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	rand.Read(img.Pix)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A cover picture over the 1 MB body limit uploads through the whole
// middleware stack (MaxBodySize, then CSRFProtect's own limit), and the rest
// of the site keeps the 1 MB limit.
func TestCoverUploadOver1MB(t *testing.T) {
	srv, d := siteServer(t)
	if _, err := d.Exec("INSERT INTO courses (id, slug, title) VALUES (1, 'estate', 'Estate Planning')"); err != nil {
		t.Fatal(err)
	}
	pic := noisePNG(t, 800, 500)
	if len(pic) <= 1<<20 {
		t.Fatalf("test picture is only %d bytes", len(pic))
	}
	multipartPost := func(path string, user int64, fields map[string]string, file []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for k, v := range fields {
			mw.WriteField(k, v)
		}
		if file != nil {
			fw, _ := mw.CreateFormFile("cover", "cover.png")
			fw.Write(file)
		}
		mw.Close()
		req := httptest.NewRequest("POST", path, &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		if user != 0 {
			req.AddCookie(&http.Cookie{Name: "session", Value: session(user)})
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}

	w := multipartPost("/admin/courses/1/cover", 1, map[string]string{"_csrf": csrfFor(session(1))}, pic)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/courses/1#cover" {
		t.Fatalf("uploading a %d-byte cover: %d to %q\n%s", len(pic), w.Code, w.Header().Get("Location"), w.Body)
	}
	var n int
	d.QueryRow("SELECT COUNT(*) FROM course_covers WHERE course_id = 1").Scan(&n)
	if n != 1 {
		t.Error("the cover wasn't stored")
	}

	// Outside /admin a big multipart body is still cut off at 1 MB: the
	// sign-in form reads multipart before anyone is signed in.
	big := multipartPost("/login", 0, map[string]string{"email": "a@example.com", "pad": strings.Repeat("x", 2<<20)}, nil)
	d.QueryRow("SELECT COUNT(*) FROM login_requests").Scan(&n)
	if n != 0 {
		t.Errorf("a 2 MB sign-in form was read (%d)", big.Code)
	}
}
