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

func TestGameTab(t *testing.T) {
	for path, want := range map[string]string{
		"/games/7":                "overview",
		"/games/7/":               "overview",
		"/games/7/portfolio":      "portfolio",
		"/games/7/history":        "portfolio",
		"/games/7/stocks":         "stocks",
		"/games/7/stocks/12":      "stocks",
		"/games/7/trade/12":       "stocks",
		"/games/7/orders":         "orders",
		"/games/7/watchlist":      "watchlist",
		"/games/7/leaderboard":    "leaderboard",
		"/games/7/sectors":        "analytics",
		"/games/7/chat":           "chat",
		"/games/7/activity":       "",
		"/games/7/export/history": "",
	} {
		if got := gameTab(path); got != want {
			t.Errorf("gameTab(%q) = %q, want %q", path, got, want)
		}
	}
}

// gameServer seeds one active game (id 1) with a player (user 1) holding
// $1M cash, a stock (id 1) with a price, and a second user (2) who hasn't
// joined. It serves the routes under /games/{id} as main.go does, with the
// user from the X-User header standing in for the session.
func gameServer(t *testing.T) (http.Handler, *db.Queries) {
	t.Helper()
	d := openTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x', 'Alice', 't1'), (2, 'b@x', 'Bob', 't2')`,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date, limit_orders)
		 VALUES (1, 1, 'Spring Cup', 'active', 'USD', '2026-01-01', '2026-12-31', 1)`,
		`INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 100000000, 100000000)`,
		`INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'ACME', 'Acme Corp', 'sp500', 'USD')`,
		`INSERT INTO stock_prices (stock_id, date, open, high, low, close, volume) VALUES (1, '2026-10-28', 1000, 1100, 900, 1050, 5000)`,
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
	r.Route("/games/{id}", func(r chi.Router) {
		r.Use(h.GameContext)
		r.Get("/", h.GameDetail)
		r.Get("/stocks/{sid}", h.StockDetail)
		r.Get("/trade/{sid}", h.TradeForm)
		r.Post("/trade/{sid}", h.TradeExecute)
	})
	return r, q
}

// TestGameOverview checks the subrouter's "/" matches /games/1, and that
// the bar marks Overview.
func TestGameOverview(t *testing.T) {
	srv, _ := gameServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/games/1", nil))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `href="/games/1" class="active">Overview`) {
		t.Error("Overview tab isn't active on the game page")
	}
}

func TestTradePageRedirectsToStockPage(t *testing.T) {
	srv, _ := gameServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/games/1/trade/1", nil))
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/games/1/stocks/1" {
		t.Fatalf("got %d to %q, want 303 to /games/1/stocks/1", w.Code, w.Header().Get("Location"))
	}
}

func TestStockPageHasGameBarAndTicket(t *testing.T) {
	srv, q := gameServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/games/1/stocks/1", nil))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{
		`class="game-bar has-dock"`,
		`class="game-dock"`, // the phone tab bar, for players only
		`Spring Cup`,
		`href="/games/1/stocks" class="active">Stocks`,
		`href="/games/1/orders"`,
		`id="ticket-form"`,
		`<option value="limit">`,
		`id="side-sell" disabled`, // holds nothing, no shorting
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}

	user, _ := q.GetUser(t.Context(), 1)
	if !user.LastGameID.Valid || user.LastGameID.Int64 != 1 {
		t.Errorf("last_game_id = %v, want 1", user.LastGameID)
	}
}

func TestStockPageWithoutJoiningHasNoTicket(t *testing.T) {
	srv, q := gameServer(t)
	req := httptest.NewRequest("GET", "/games/1/stocks/1", nil)
	req.Header.Set("X-User", "2")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `id="ticket-form"`) {
		t.Error("non-member was shown the order ticket")
	}
	if !strings.Contains(body, "Join it") {
		t.Error("non-member wasn't told to join")
	}
	if user, _ := q.GetUser(t.Context(), 2); user.LastGameID.Valid {
		t.Error("viewing a game you haven't joined made it your current game")
	}
}

// setMarketOpen opens or closes the US market for market orders, for one test.
func setMarketOpen(t *testing.T, open bool) {
	old := queuesMarketOrder
	queuesMarketOrder = func(string) bool { return !open }
	t.Cleanup(func() { queuesMarketOrder = old })
}

func postTicket(srv http.Handler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/games/1/trade/1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

func TestMarketOrderQueuesWhileClosed(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, false)
	w := postTicket(srv, url.Values{"side": {"buy"}, "order_type": {"market"}, "quantity": {"5"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/games/1/orders" {
		t.Fatalf("got %d to %q, want 303 to /games/1/orders", w.Code, w.Header().Get("Location"))
	}

	p, _ := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	orders, _ := q.ListOpenLimitOrders(t.Context(), p.ID)
	if len(orders) != 1 || orders[0].Kind != "market" || orders[0].Quantity != 5 || orders[0].Type != "buy" {
		t.Errorf("open orders = %+v, want one queued market buy of 5", orders)
	}
	if p.CashBalance != 100000000 {
		t.Errorf("cash = %d; a queued order must not spend cash until it fills", p.CashBalance)
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "queued") {
		t.Errorf("flash doesn't tell the player the order is queued: %q", w.Header().Get("Set-Cookie"))
	}
}

func TestMarketOrderFillsWhileOpen(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, true)
	w := postTicket(srv, url.Values{"side": {"buy"}, "order_type": {"market"}, "quantity": {"5"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/games/1/stocks/1" {
		t.Fatalf("got %d to %q, want 303 to /games/1/stocks/1", w.Code, w.Header().Get("Location"))
	}
	p, _ := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	if h, _ := q.GetHolding(t.Context(), db.GetHoldingParams{ParticipantID: p.ID, StockID: 1}); h.Quantity != 5 {
		t.Errorf("holding = %d, want 5", h.Quantity)
	}
}

func TestTicketSaysWhenOrdersQueue(t *testing.T) {
	for _, open := range []bool{false, true} {
		srv, _ := gameServer(t)
		setMarketOpen(t, open)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("GET", "/games/1/stocks/1", nil))
		body := w.Body.String()
		if got := strings.Contains(body, `id="queued-note"`); got == open {
			t.Errorf("market open %v: queued note shown = %v", open, got)
		}
		if got := strings.Contains(body, "at the next open"); got == open {
			t.Errorf("market open %v: order type says next open = %v", open, got)
		}
	}
}

func TestTicketLimitOrderPlacesOrderNotTrade(t *testing.T) {
	srv, q := gameServer(t)
	form := url.Values{"side": {"buy"}, "order_type": {"limit"}, "quantity": {"5"}, "limit_price": {"10.25"}}
	req := httptest.NewRequest("POST", "/games/1/trade/1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/games/1/orders" {
		t.Fatalf("got %d to %q, want 303 to /games/1/orders", w.Code, w.Header().Get("Location"))
	}

	p, err := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	orders, _ := q.ListOpenLimitOrders(t.Context(), p.ID)
	if len(orders) != 1 || orders[0].Quantity != 5 || orders[0].LimitPrice != 1025 || orders[0].Type != "buy" {
		t.Errorf("open orders = %+v, want one buy of 5 at 1025", orders)
	}
	if p.CashBalance != 100000000 {
		t.Errorf("cash = %d; a limit order must not spend cash until it fills", p.CashBalance)
	}
}
