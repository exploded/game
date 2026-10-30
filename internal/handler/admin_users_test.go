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
	"github.com/go-chi/chi/v5"
)

// adminUsersServer seeds an admin (1), Bob (2, signed in, with a trade),
// and a second admin, Cat (3). Requests are made as user 1.
func adminUsersServer(t *testing.T) (http.Handler, *sql.DB, *db.Queries) {
	t.Helper()
	d := openTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token, is_admin, created_at) VALUES
			(1, 'admin@x.com', 'Admin', 't1', 1, '2026-10-01 00:00:00'),
			(2, 'bob@x.com', 'Bob', 't2', 0, '2026-10-02 00:00:00'),
			(3, 'cat@x.com', 'Cat', 't3', 1, '2026-10-03 00:00:00')`,
		`INSERT INTO sessions (id, user_id, expires_at) VALUES ('s2', 2, datetime('now', '+1 day'))`,
		`INSERT INTO games (id, created_by, name, status, start_date, end_date) VALUES (1, 1, 'October 2026', 'active', '2026-10-01', '2026-10-31')`,
		`INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 2, 100000000, 100000000)`,
		`INSERT INTO stocks (id, symbol, name, market) VALUES (1, 'AAPL', 'Apple', 'sp500')`,
		`INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total) VALUES (1, 1, 'buy', 1, 100, 100, 100)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			admin, _ := q.GetUser(r.Context(), 1)
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), &admin)))
		})
	})
	r.Get("/admin/users", h.AdminUsers)
	r.Post("/admin/users/{id}/block", h.AdminBlockToggle)
	return r, d, q
}

func TestAdminUsersListsActivity(t *testing.T) {
	srv, _, _ := adminUsersServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/admin/users?sort=trades", nil))
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("GET /admin/users = %d\n%s", w.Code, body)
	}
	bob, cat := strings.Index(body, "bob@x.com"), strings.Index(body, "cat@x.com")
	if bob < 0 || cat < 0 || bob > cat {
		t.Errorf("sorted by trades, Bob (1 trade) isn't listed before Cat (none)")
	}
	if !strings.Contains(body, `data-label="Trades">1<`) {
		t.Errorf("Bob's trade isn't counted:\n%s", body)
	}
	if !strings.Contains(body, `href="/admin/users" class="btn btn-primary" aria-current="page">Users`) ||
		!strings.Contains(body, `href="/admin/learn" class="btn btn-primary">Learn`) {
		t.Errorf("the admin links are missing, or Users isn't marked current:\n%s", body)
	}
}

func TestBlockUser(t *testing.T) {
	srv, d, q := adminUsersServer(t)
	block := func(id string) string {
		w := post(srv, "/admin/users/"+id+"/block", url.Values{"sort": {"trades"}})
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/users?sort=trades" {
			t.Fatalf("block %s = %d to %q", id, w.Code, w.Header().Get("Location"))
		}
		f := cookie(w, "flash")
		if f == nil {
			t.Fatalf("block %s: no flash", id)
		}
		msg, _ := url.QueryUnescape(f.Value)
		return msg
	}

	if msg := block("2"); !strings.Contains(msg, "Blocked Bob") {
		t.Errorf("blocking Bob: %q", msg)
	}
	if bob, _ := q.GetUser(t.Context(), 2); !bob.BlockedAt.Valid {
		t.Error("Bob isn't blocked")
	}
	var sessions int
	d.QueryRow("SELECT COUNT(*) FROM sessions WHERE user_id = 2").Scan(&sessions)
	if sessions != 0 {
		t.Errorf("Bob still has %d sessions", sessions)
	}

	if msg := block("1"); !strings.Contains(msg, "can't block yourself") {
		t.Errorf("blocking yourself: %q", msg)
	}
	if msg := block("3"); !strings.Contains(msg, "Remove their admin first") {
		t.Errorf("blocking an admin: %q", msg)
	}
	for _, id := range []int64{1, 3} {
		if u, _ := q.GetUser(t.Context(), id); u.BlockedAt.Valid {
			t.Errorf("user %d was blocked", id)
		}
	}

	if msg := block("2"); !strings.Contains(msg, "Unblocked Bob") {
		t.Errorf("unblocking Bob: %q", msg)
	}
	if bob, _ := q.GetUser(t.Context(), 2); bob.BlockedAt.Valid {
		t.Error("Bob is still blocked")
	}
}

func TestBlockedSessionIsSignedOut(t *testing.T) {
	_, _, q := adminUsersServer(t)
	if err := q.BlockUser(t.Context(), 2); err != nil {
		t.Fatal(err)
	}
	// The session row survives here, as if Bob was blocked some other way.
	reached := false
	srv := auth.RequireAuth(q, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	req := httptest.NewRequest("GET", "/dashboard", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "s2"})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if reached || !strings.HasPrefix(w.Header().Get("Location"), "/login") {
		t.Errorf("blocked session: reached = %v, Location %q", reached, w.Header().Get("Location"))
	}
}

func TestBlockedUserIsHidden(t *testing.T) {
	_, d, q := adminUsersServer(t)
	ctx := t.Context()
	for _, stmt := range []string{
		`INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (1, 3, 100000000, 90000000)`,
		`INSERT INTO game_messages (game_id, user_id, message) VALUES (1, 2, 'from Bob'), (1, 3, 'from Cat')`,
		`INSERT INTO lobby_messages (user_id, message) VALUES (2, 'from Bob'), (3, 'from Cat')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := q.BlockUser(ctx, 2); err != nil {
		t.Fatal(err)
	}

	board, _ := q.ListPublicParticipantsByGame(ctx, 1)
	all, _ := q.ListParticipantsByGame(ctx, 1)
	if len(board) != 1 || board[0].UserID != 3 || len(all) != 1 {
		t.Errorf("standings: %d public, %d in all, want only Cat", len(board), len(all))
	}
	msgs, _ := q.ListMessages(ctx, db.ListMessagesParams{GameID: 1, Limit: 10})
	n, _ := q.CountMessages(ctx, 1)
	if len(msgs) != 1 || msgs[0].UserID != 3 || n != 1 {
		t.Errorf("game chat: %d messages, count %d, want only Cat's", len(msgs), n)
	}
	lobby, _ := q.ListLobbyMessages(ctx, db.ListLobbyMessagesParams{Limit: 10})
	n, _ = q.CountLobbyMessages(ctx)
	if len(lobby) != 1 || lobby[0].UserID != 3 || n != 1 {
		t.Errorf("lobby: %d messages, count %d, want only Cat's", len(lobby), n)
	}

	// Unblocking brings it all back.
	if err := q.UnblockUser(ctx, 2); err != nil {
		t.Fatal(err)
	}
	if board, _ := q.ListPublicParticipantsByGame(ctx, 1); len(board) != 2 {
		t.Errorf("after unblocking, %d on the leaderboard, want 2", len(board))
	}
	if n, _ := q.CountLobbyMessages(ctx); n != 2 {
		t.Errorf("after unblocking, %d lobby messages, want 2", n)
	}
}

func TestBlockedUserCantSignIn(t *testing.T) {
	srv, d, q := loginServer(t)
	user, err := q.CreateUser(t.Context(), db.CreateUserParams{Email: "bob@example.com", Name: "Bob", EmailToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	// Blocked after asking for a code: the code no longer works.
	req, code, _ := requestLogin(t, srv, d, "bob@example.com")
	if err := q.BlockUser(t.Context(), user.ID); err != nil {
		t.Fatal(err)
	}
	w := post(srv, "/login/code", url.Values{"code": {code}}, req)
	if w.Header().Get("Location") != "/login" || cookie(w, "session") != nil {
		t.Errorf("blocked code sign-in: %d to %q", w.Code, w.Header().Get("Location"))
	}

	// And no more sign-in emails are sent.
	var before, after int
	d.QueryRow("SELECT COUNT(*) FROM email_outbox").Scan(&before)
	w = post(srv, "/login", url.Values{"email": {"Bob@Example.com"}})
	d.QueryRow("SELECT COUNT(*) FROM email_outbox").Scan(&after)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "This account is blocked") || after != before {
		t.Errorf("POST /login when blocked = %d, %d emails queued\n%s", w.Code, after-before, w.Body)
	}
}
