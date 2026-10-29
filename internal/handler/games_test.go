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
// admin.
func quarterServer(t *testing.T) (http.Handler, *sql.DB) {
	t.Helper()
	d := openTestDB(t)
	this := market.QuarterOf(time.Now().UTC())
	next := this.Next()
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token, is_admin) VALUES (1, 'a@x', 'Alice', 't1', 1)`,
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
			user, err := q.GetUser(r.Context(), 1)
			if err != nil {
				t.Fatalf("GetUser: %v", err)
			}
			next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), &user)))
		})
	})
	r.Get("/games", h.GamesList)
	r.Get("/dashboard", h.Dashboard)
	r.Post("/admin/games/{gid}", h.AdminGameUpdate)
	return r, d
}

func TestGamesPageShowsQuarterAndPastGames(t *testing.T) {
	srv, _ := quarterServer(t)
	this := market.QuarterOf(time.Now().UTC())
	for _, path := range []string{"/games", "/dashboard"} {
		body := getPage(t, srv, path, "1")
		for _, want := range []string{"This quarter", this.Name(), "Next game", this.Next().Name(), `action="/games/2/join"`} {
			if !strings.Contains(body, want) {
				t.Errorf("%s doesn't contain %q", path, want)
			}
		}
		if strings.Contains(body, `action="/games/1/join"`) {
			t.Errorf("%s offers to join a game Alice is in", path)
		}
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
		"allow_short": {"1"}, // limit_orders unticked
	}
	for _, id := range []string{"1", "2", "3"} {
		if w := postSettings(srv, id, form); w.Code != http.StatusSeeOther {
			t.Fatalf("game %s: status %d, want 303", id, w.Code)
		}
	}

	type settings struct {
		name, markets, currency            string
		balance, fee, bonus, short, limits int
	}
	get := func(id int) settings {
		var s settings
		if err := d.QueryRow(`SELECT name, markets, base_currency, starting_balance, trade_fee, referral_bonus_pct, allow_short, limit_orders FROM games WHERE id = ?`, id).
			Scan(&s.name, &s.markets, &s.currency, &s.balance, &s.fee, &s.bonus, &s.short, &s.limits); err != nil {
			t.Fatal(err)
		}
		return s
	}
	// Game 1 has a player, so its starting cash and currency stay put.
	if got, want := get(1), (settings{"Summer Cup", "sp500", "AUD", 100000000, 995, 2, 1, 0}); got != want {
		t.Errorf("running game with a player: got %+v, want %+v", got, want)
	}
	if got, want := get(2), (settings{"Summer Cup", "sp500", "USD", 50000000, 995, 2, 1, 0}); got != want {
		t.Errorf("next game: got %+v, want %+v", got, want)
	}
	// A finished game can't change.
	if got := get(3); got.name != "Spring Cup" || got.fee != 0 {
		t.Errorf("finished game changed: %+v", got)
	}
}
