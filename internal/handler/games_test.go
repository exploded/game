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
	"github.com/exploded/game/internal/market"
	"github.com/go-chi/chi/v5"
)

// quarterServer seeds this quarter's game (id 1, which Alice has joined), the
// next quarter's (id 2) and a finished game (id 3). Alice (user 1) is an
// admin; Bob (user 2, X-User: 2) hasn't joined anything.
func quarterServer(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	d := openTestDB(t)
	this := market.QuarterOf(time.Now().UTC())
	next := this.Next()
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token, is_admin) VALUES (1, 'a@x', 'Alice', 't1', 1), (2, 'b@x', 'Bob', 't2', 0)`,
		`INSERT INTO games (id, created_by, name, status, start_date, end_date, quarter, trade_fee, limit_orders)
		 VALUES (1, 1, '` + this.Name() + `', 'active', '` + this.StartDate() + `', '` + this.EndDate() + `', '` + this.Key() + `', 0, 1),
		        (2, 1, '` + next.Name() + `', 'pending', '` + next.StartDate() + `', '` + next.EndDate() + `', '` + next.Key() + `', 0, 1),
		        (3, 1, 'Spring Cup', 'finished', '2026-03-01', '2026-03-31', NULL, 0, 0)`,
		`INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 100000000, 100000000)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := int64(1)
			if r.Header.Get("X-User") == "2" {
				id = 2
			}
			user, err := q.GetUser(r.Context(), id)
			if err != nil {
				t.Fatalf("GetUser: %v", err)
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), &user)))
		})
	})
	r.Get("/games", h.GamesList)
	r.Get("/dashboard", h.Dashboard)
	r.Route("/games/{id}", func(r chi.Router) {
		r.Use(h.GameContext)
		r.Get("/", h.GameDetail)
	})
	r.Post("/admin/games/{gid}", h.AdminGameUpdate)
	return r, d
}

func TestGamesPageShowsQuarterAndPastGames(t *testing.T) {
	srv, _ := quarterServer(t)
	this := market.QuarterOf(time.Now().UTC())
	body := getPage(t, srv, "/games", "1")
	for _, want := range []string{"This quarter", this.Name(), "Next game", this.Next().Name(), `action="/games/2/join"`} {
		if !strings.Contains(body, want) {
			t.Errorf("/games doesn't contain %q", want)
		}
	}
	if strings.Contains(body, `action="/games/1/join"`) {
		t.Error("/games offers to join a game Alice is in")
	}
	if body := getPage(t, srv, "/games", "1"); !strings.Contains(body, "Spring Cup") || !strings.Contains(body, "/games/3/leaderboard") {
		t.Error("/games doesn't list the finished game with its standings")
	}
}

func postSettings(srv http.Handler, game string, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/admin/games/"+game, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestAdminChangesGameSettings(t *testing.T) {
	srv, d := quarterServer(t)
	form := url.Values{
		"name": {"Summer Cup"}, "markets": {"sp500"}, "starting_balance": {"500000"},
		"base_currency": {"USD"}, "trade_fee": {"9.95"}, "referral_bonus_pct": {"2"},
		"credit_interest_rate": {"4.35"}, "allow_short": {"1"}, // limit_orders and after_hours_trading unticked
	}
	for _, id := range []string{"1", "2", "3"} {
		if w := postSettings(srv, id, form); w.Code != http.StatusSeeOther {
			t.Fatalf("game %s: status %d, want 303", id, w.Code)
		}
	}

	type settings struct {
		name, markets, currency                                  string
		balance, fee, bonus, short, limits, interest, afterHours int
	}
	get := func(id int) settings {
		var s settings
		if err := d.QueryRow(`SELECT name, markets, base_currency, starting_balance, trade_fee, referral_bonus_pct, allow_short, limit_orders, credit_interest_rate, after_hours_trading FROM games WHERE id = ?`, id).
			Scan(&s.name, &s.markets, &s.currency, &s.balance, &s.fee, &s.bonus, &s.short, &s.limits, &s.interest, &s.afterHours); err != nil {
			t.Fatal(err)
		}
		return s
	}
	// Game 1 has a player, so its starting cash and currency stay put.
	if got, want := get(1), (settings{"Summer Cup", "sp500", "AUD", 100000000, 995, 2, 1, 0, 435, 0}); got != want {
		t.Errorf("running game with a player: got %+v, want %+v", got, want)
	}
	if got, want := get(2), (settings{"Summer Cup", "sp500", "USD", 50000000, 995, 2, 1, 0, 435, 0}); got != want {
		t.Errorf("next game: got %+v, want %+v", got, want)
	}
	// A finished game can't change.
	if got := get(3); got.name != "Spring Cup" || got.fee != 0 {
		t.Errorf("finished game changed: %+v", got)
	}
}

// TestDashboardJoinsCurrentGame: a player who has never joined anything
// lands in this quarter's game with its starting cash.
func TestDashboardJoinsCurrentGame(t *testing.T) {
	srv, d := quarterServer(t)
	for range 2 { // the second visit mustn't join again
		req := httptest.NewRequest("GET", "/dashboard", nil)
		req.Header.Set("X-User", "2")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/games/1" {
			t.Fatalf("got %d to %q, want 303 to /games/1", w.Code, w.Header().Get("Location"))
		}
	}
	var n int
	var cash int64
	if err := d.QueryRow("SELECT COUNT(*), SUM(cash_balance) FROM participants WHERE game_id = 1 AND user_id = 2").Scan(&n, &cash); err != nil || n != 1 || cash != 100000000 {
		t.Errorf("Bob in game 1: %d rows with %d cash (%v), want 1 with 100000000", n, cash, err)
	}
}

// TestGamePageJoinsOnlyCurrentGame: opening this quarter's game joins you;
// opening the next or a past game doesn't.
func TestGamePageJoinsOnlyCurrentGame(t *testing.T) {
	srv, d := quarterServer(t)
	for _, id := range []string{"2", "3", "1"} {
		getPage(t, srv, "/games/"+id, "2")
	}
	var games string
	d.QueryRow("SELECT group_concat(game_id) FROM participants WHERE user_id = 2").Scan(&games)
	if games != "1" {
		t.Errorf("Bob is in games %q, want just 1", games)
	}
}
