package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// The sessions table holds a hash of each cookie, never the cookie, and
// signing in, being signed in and signing out all go through it.
func TestSessionsStoreHashes(t *testing.T) {
	d := openTestDB(t)
	q := db.New(d)
	if _, err := d.Exec("INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x.com', 'A', 't')"); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	if err := auth.StartSession(w, httptest.NewRequest("POST", "/login/code", nil), q, 1); err != nil {
		t.Fatal(err)
	}
	c := cookie(w, "session")
	if c == nil || len(c.Value) != 64 {
		t.Fatalf("session cookie %v", c)
	}
	var stored string
	d.QueryRow("SELECT id FROM sessions WHERE user_id = 1").Scan(&stored)
	if stored == c.Value || stored != auth.SessionKey(c.Value) {
		t.Fatalf("stored %q for cookie %q, want its SHA-256", stored, c.Value)
	}

	signedIn := func(value string) bool {
		var user *db.User
		srv := auth.RequireAuth(q, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user = auth.UserFromContext(r.Context())
		}))
		req := httptest.NewRequest("GET", "/dashboard", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: value})
		srv.ServeHTTP(httptest.NewRecorder(), req)
		return user != nil && user.ID == 1
	}
	if !signedIn(c.Value) {
		t.Error("the cookie doesn't sign in")
	}
	if signedIn(stored) {
		t.Error("the stored hash works as a cookie")
	}

	req := httptest.NewRequest("POST", "/logout", nil)
	req.AddCookie(c)
	auth.HandleLogout(q).ServeHTTP(httptest.NewRecorder(), req)
	var n int
	d.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&n)
	if n != 0 || signedIn(c.Value) {
		t.Errorf("%d sessions left after signing out", n)
	}
}

// hash-session-ids hashes the sessions stored before it, once, so nobody is
// signed out.
func TestHashSessionIDsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := OpenDB(path, game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x.com', 'A', 't')",
		"INSERT INTO sessions (id, user_id, expires_at) VALUES ('old-cookie', 1, datetime('now', '+1 day'))",
		"DELETE FROM schema_migrations WHERE name = 'hash-session-ids'",
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	d.Close()

	for range 2 { // and opening again changes nothing
		d = openTestDBAt(t, path)
		var id string
		d.QueryRow("SELECT id FROM sessions WHERE user_id = 1").Scan(&id)
		if id != auth.SessionKey("old-cookie") {
			t.Fatalf("session id %q, want the hash of the old cookie", id)
		}
		var user *db.User
		srv := auth.RequireAuth(db.New(d), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user = auth.UserFromContext(r.Context())
		}))
		req := httptest.NewRequest("GET", "/dashboard", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "old-cookie"})
		srv.ServeHTTP(httptest.NewRecorder(), req)
		if user == nil {
			t.Fatal("the old cookie doesn't sign in after the migration")
		}
		d.Close()
	}
}
