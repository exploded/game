package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/go-chi/chi/v5"
)

func TestAdminDeletesLobbyMessage(t *testing.T) {
	d := openTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token, is_admin) VALUES
			(1, 'admin@x.com', 'Admin', 't1', 1),
			(2, 'bob@x.com', 'Bob', 't2', 0)`,
		`INSERT INTO lobby_messages (id, user_id, message) VALUES (1, 2, 'buy my course'), (2, 2, 'hello all')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	as := func(id int64) http.Handler {
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u, _ := q.GetUser(r.Context(), id)
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), &u)))
			})
		})
		r.Get("/lobby", h.LobbyPage)
		r.Post("/admin/lobby/{mid}/delete", h.AdminLobbyDelete)
		return r
	}
	page := func(srv http.Handler) string {
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("GET", "/lobby", nil))
		if w.Code != http.StatusOK {
			t.Fatalf("GET /lobby = %d\n%s", w.Code, w.Body.String())
		}
		return w.Body.String()
	}

	if body := page(as(2)); strings.Contains(body, "/admin/lobby/") {
		t.Errorf("a player sees the Delete button")
	}
	if body := page(as(1)); !strings.Contains(body, `action="/admin/lobby/1/delete"`) {
		t.Errorf("the admin doesn't see the Delete button:\n%s", body)
	}

	w := post(as(1), "/admin/lobby/1/delete", url.Values{})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/lobby" {
		t.Fatalf("delete = %d to %q", w.Code, w.Header().Get("Location"))
	}
	if body := page(as(1)); strings.Contains(body, "buy my course") || !strings.Contains(body, "hello all") {
		t.Errorf("the wrong messages are left:\n%s", body)
	}
}
