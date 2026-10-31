package handler

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/moderate"
	"github.com/go-chi/chi/v5"
)

// fakeModerator blocks messages mentioning Telegram, and fails on "outage".
type fakeModerator struct{ seen []string }

func (f *fakeModerator) Check(_ context.Context, msg string) (moderate.Verdict, error) {
	f.seen = append(f.seen, msg)
	switch {
	case strings.Contains(msg, "outage"):
		return moderate.Verdict{}, errors.New("API down")
	case strings.Contains(strings.ToLower(msg), "telegram"):
		return moderate.Verdict{Reason: "It moves players to an outside app."}, nil
	}
	return moderate.Verdict{Allow: true}, nil
}

// lobbyServer has an admin (1), Bob (2) and a second admin, Cat (3), with
// every lobby route, and as(id) to make requests as one of them.
func lobbyServer(t *testing.T) (as func(int64) http.Handler, q *db.Queries, mod *fakeModerator) {
	t.Helper()
	as, _, q, mod = lobbyServerDB(t)
	return as, q, mod
}

// lobbyServerDB is lobbyServer with the database, for tests that change
// times.
func lobbyServerDB(t *testing.T) (as func(int64) http.Handler, d *sql.DB, q *db.Queries, mod *fakeModerator) {
	t.Helper()
	d = openTestDB(t)
	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token, is_admin) VALUES
		(1, 'admin@x.com', 'Admin', 't1', 1),
		(2, 'bob@x.com', 'Bob', 't2', 0),
		(3, 'cat@x.com', 'Cat', 't3', 1)`); err != nil {
		t.Fatal(err)
	}
	q = db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	mod = &fakeModerator{}
	h.SetModerator(mod)
	as = func(id int64) http.Handler {
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				u, _ := q.GetUser(r.Context(), id)
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), &u)))
			})
		})
		r.Get("/lobby", h.LobbyPage)
		r.Post("/lobby", h.LobbySend)
		r.Get("/admin", h.AdminDashboard)
		r.Post("/admin/lobby/{mid}/delete", h.AdminLobbyDelete)
		r.Post("/admin/lobby/{mid}/unblock", h.AdminLobbyUnblock)
		r.Post("/admin/lobby/{mid}/block-user", h.AdminLobbyBlockUser)
		return r
	}
	return as, d, q, mod
}

func lobbyGet(t *testing.T, srv http.Handler, path string) string {
	t.Helper()
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d\n%s", path, w.Code, w.Body.String())
	}
	return w.Body.String()
}

func sendLobby(srv http.Handler, msg string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/lobby", strings.NewReader(url.Values{"message": {msg}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestAdminDeletesLobbyMessage(t *testing.T) {
	as, q, _ := lobbyServer(t)
	for _, m := range []string{"buy my course", "hello all"} {
		if _, err := q.CreateLobbyMessage(t.Context(), db.CreateLobbyMessageParams{UserID: 2, Message: m}); err != nil {
			t.Fatal(err)
		}
	}

	if body := lobbyGet(t, as(2), "/lobby"); strings.Contains(body, "/admin/lobby/") {
		t.Errorf("a player sees the Delete button")
	}
	if body := lobbyGet(t, as(1), "/lobby"); !strings.Contains(body, `action="/admin/lobby/1/delete"`) {
		t.Errorf("the admin doesn't see the Delete button:\n%s", body)
	}

	w := post(as(1), "/admin/lobby/1/delete", url.Values{})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/lobby" {
		t.Fatalf("delete = %d to %q", w.Code, w.Header().Get("Location"))
	}
	if body := lobbyGet(t, as(1), "/lobby"); strings.Contains(body, "buy my course") || !strings.Contains(body, "hello all") {
		t.Errorf("the wrong messages are left:\n%s", body)
	}
}

func TestLobbyModeration(t *testing.T) {
	as, q, mod := lobbyServer(t)
	bob := as(2)
	posted := func() int64 {
		n, err := q.CountLobbyMessages(t.Context(), db.CountLobbyMessagesParams{ShowPosted: 1})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}

	if w := sendLobby(bob, "  NVDA looks cheap after the dip  "); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "NVDA looks cheap") {
		t.Fatalf("allowed message = %d\n%s", w.Code, w.Body.String())
	}
	if got := mod.seen[0]; got != "NVDA looks cheap after the dip" {
		t.Errorf("checked %q, want the trimmed message", got)
	}
	if n := posted(); n != 1 {
		t.Fatalf("%d messages posted after an allowed one, want 1", n)
	}

	w := sendLobby(bob, "Join my Telegram for signals")
	if w.Code != http.StatusNoContent {
		t.Fatalf("blocked message = %d, want 204", w.Code)
	}
	if trig := w.Header().Get("HX-Trigger"); !strings.Contains(trig, "It moves players to an outside app.") || !strings.Contains(trig, `"error"`) {
		t.Errorf("HX-Trigger = %q, want an error toast with the reason", trig)
	}
	if n := posted(); n != 1 {
		t.Errorf("a blocked message was posted")
	}
	if n, _ := q.CountBlockedLobbyMessages(t.Context()); n != 1 {
		t.Errorf("%d blocked messages kept for review, want 1", n)
	}

	if w := sendLobby(bob, "posting during an outage"); w.Code != http.StatusOK {
		t.Fatalf("message during an outage = %d, want it posted", w.Code)
	}
	if n := posted(); n != 2 {
		t.Errorf("%d messages posted after an outage, want 2: it should post unchecked", n)
	}
}

func TestLobbyRateLimit(t *testing.T) {
	as, d, q, mod := lobbyServerDB(t)
	bob := as(2)
	tooFast := func(msg, want string) {
		t.Helper()
		checked := len(mod.seen)
		w := sendLobby(bob, msg)
		if w.Code != http.StatusNoContent || !strings.Contains(w.Header().Get("HX-Trigger"), want) {
			t.Errorf("%q = %d, HX-Trigger %q, want a toast saying %q", msg, w.Code, w.Header().Get("HX-Trigger"), want)
		}
		if len(mod.seen) != checked {
			t.Errorf("%q went to moderation past the limit", msg)
		}
	}

	// Blocked messages count towards the limit.
	for i := range maxLobbyPerMinute {
		msg := "hello " + strconv.Itoa(i)
		if i == 0 {
			msg = "join my telegram"
		}
		sendLobby(bob, msg)
	}
	tooFast("one more", "too fast")
	// Other players aren't held up.
	if w := sendLobby(as(3), "hi bob"); w.Code != http.StatusOK {
		t.Errorf("Cat's message = %d, want it posted", w.Code)
	}

	// An hour later the minute's limit has passed, but not the day's.
	if _, err := d.Exec(`UPDATE lobby_messages SET created_at = datetime('now', '-1 hour')`); err != nil {
		t.Fatal(err)
	}
	if w := sendLobby(bob, "back again"); w.Code != http.StatusOK {
		t.Fatalf("message after an hour = %d, want it posted", w.Code)
	}
	for range maxLobbyPerDay - maxLobbyPerMinute - 1 {
		if _, err := q.CreateLobbyMessage(t.Context(), db.CreateLobbyMessageParams{UserID: 2, Message: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec(`UPDATE lobby_messages SET created_at = datetime('now', '-1 hour')`); err != nil {
		t.Fatal(err)
	}
	tooFast("message 101", "100 messages a day")
}

func TestLobbyCutsLongMessagesOnCharacters(t *testing.T) {
	as, _, mod := lobbyServer(t)
	sendLobby(as(2), strings.Repeat("é", maxLobbyMessage+10))
	got := mod.seen[0]
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != maxLobbyMessage {
		t.Errorf("checked %d characters (valid UTF-8 %v), want %d", utf8.RuneCountInString(got), utf8.ValidString(got), maxLobbyMessage)
	}
}

func TestAdminReviewsBlockedLobbyMessages(t *testing.T) {
	as, q, _ := lobbyServer(t)
	admin, bob := as(1), as(2)
	sendLobby(bob, "hello all")
	sendLobby(bob, "Join my Telegram for signals") // message 2, blocked

	// Players don't see it, even asking for the blocked list.
	for _, path := range []string{"/lobby", "/lobby?blocked=1"} {
		body := lobbyGet(t, bob, path)
		if strings.Contains(body, "Telegram") || !strings.Contains(body, "hello all") {
			t.Errorf("Bob's %s shows the wrong messages:\n%s", path, body)
		}
	}

	// The admin sees it in the lobby, marked, with its reason and the buttons.
	body := lobbyGet(t, admin, "/lobby")
	for _, want := range []string{"Telegram", "hello all", "Blocked", "It moves players to an outside app.",
		`action="/admin/lobby/2/unblock"`, `action="/admin/lobby/2/block-user"`, "1 blocked"} {
		if !strings.Contains(body, want) {
			t.Errorf("the admin's lobby has no %q", want)
		}
	}
	if strings.Contains(body, `action="/admin/lobby/1/block-user"`) {
		t.Errorf("Block user is on a posted message")
	}
	// The blocked list has only it, and the buttons come back to the list.
	body = lobbyGet(t, admin, "/lobby?blocked=1")
	if !strings.Contains(body, "Telegram") || strings.Contains(body, "hello all") || !strings.Contains(body, `name="blocked" value="1"`) {
		t.Errorf("the blocked list is wrong:\n%s", body)
	}
	// The admin bar counts it.
	if body := lobbyGet(t, admin, "/admin"); !strings.Contains(body, `href="/lobby?blocked=1"`) || !strings.Contains(body, "Lobby (1)") {
		t.Errorf("the admin bar doesn't count the blocked message")
	}

	// Unblock posts it for everyone and keeps the admin in the list.
	w := post(admin, "/admin/lobby/2/unblock", url.Values{"blocked": {"1"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/lobby?blocked=1" {
		t.Fatalf("unblock = %d to %q", w.Code, w.Header().Get("Location"))
	}
	if body := lobbyGet(t, bob, "/lobby"); !strings.Contains(body, "Telegram") {
		t.Errorf("an unblocked message isn't posted")
	}
	if n, _ := q.CountBlockedLobbyMessages(t.Context()); n != 0 {
		t.Errorf("%d blocked after unblocking, want 0", n)
	}

	// Block user blocks the author, whose messages leave the lobby.
	sendLobby(bob, "telegram again") // message 3
	w = post(admin, "/admin/lobby/3/block-user", url.Values{})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/lobby" {
		t.Fatalf("block user = %d to %q", w.Code, w.Header().Get("Location"))
	}
	if u, _ := q.GetUser(t.Context(), 2); !u.BlockedAt.Valid {
		t.Error("Bob isn't blocked")
	}
	if n, _ := q.CountBlockedLobbyMessages(t.Context()); n != 0 {
		t.Errorf("%d blocked after blocking the author, want 0", n)
	}
	if body := lobbyGet(t, admin, "/lobby"); strings.Contains(body, "hello all") {
		t.Errorf("a blocked user's messages are still in the lobby")
	}

	// It won't block an admin.
	sendLobby(as(3), "my telegram") // message 4, from Cat
	w = post(admin, "/admin/lobby/4/block-user", url.Values{})
	if f := cookie(w, "flash"); f == nil || !strings.Contains(f.Value, "is an admin") {
		t.Errorf("blocking an admin's message: flash %v", f)
	}
	if u, _ := q.GetUser(t.Context(), 3); u.BlockedAt.Valid {
		t.Error("Cat, an admin, was blocked")
	}
}
