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

func TestGameTab(t *testing.T) {
	for path, want := range map[string]string{
		"/games/7":                "home",
		"/games/7/":               "home",
		"/games/7/rules":          "rules",
		"/games/7/portfolio":      "portfolio",
		"/games/7/history":        "portfolio",
		"/games/7/stocks":         "stocks",
		"/games/7/stocks/12":      "stocks",
		"/games/7/trade/12":       "stocks",
		"/games/7/orders":         "orders",
		"/games/7/watchlist":      "watchlist",
		"/games/7/leaderboard":    "leaderboard",
		"/games/7/sectors":        "analytics",
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
	srv, q, _ := gameServerDB(t)
	return srv, q
}

// gameServerDB is gameServer, also returning the database for seeding.
func gameServerDB(t *testing.T) (http.Handler, *db.Queries, *sql.DB) {
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
		r.Get("/rules", h.GameRules)
		r.Get("/portfolio", h.Portfolio)
		r.Get("/history", h.TransactionHistory)
		r.Get("/stocks", h.StocksBrowse)
		r.Get("/stocks/{sid}", h.StockDetail)
		r.Get("/trade/{sid}", h.TradeForm)
		r.Post("/trade/{sid}", h.TradeExecute)
	})
	return r, q, d
}

func getPage(t *testing.T, srv http.Handler, path, user string) string {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	if user != "" {
		req.Header.Set("X-User", user)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("GET %s: status %d: %s", path, w.Code, w.Body.String())
	}
	return w.Body.String()
}

// TestGameHomeIsPortfolio checks a player's game page leads with their
// holdings: today's and total moves for each stock, and their rank.
func TestGameHomeIsPortfolio(t *testing.T) {
	srv, q := gameServer(t)
	p, _ := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	// 10 ACME bought at $9.00, closed at $10.00 then $10.50.
	if err := q.UpsertHolding(t.Context(), db.UpsertHoldingParams{
		ParticipantID: p.ID, StockID: 1, Quantity: 10, AvgCost: 900, CostBasis: 9000, CurrentValue: 10500,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.JoinGame(t.Context(), db.JoinGameParams{GameID: 1, UserID: 2, CashBalance: 100000000, PortfolioValue: 100100000}); err != nil {
		t.Fatal(err)
	}
	if err := q.UpsertPrice(t.Context(), db.UpsertPriceParams{StockID: 1, Date: "2026-10-27", Open: 1000, High: 1000, Low: 1000, Close: 1000}); err != nil {
		t.Fatal(err)
	}

	body := getPage(t, srv, "/games/1", "")
	for _, want := range []string{
		`href="/games/1" class="active">Portfolio`,
		`<span class="rank-place">2nd</span>`,
		`$1,000.00 behind Bob`,
		`<span class="pill pill-up">&#43;5.00%</span> <span class="c-amt">&#43;$5.00</span>`,   // today
		`<span class="pill pill-up">&#43;16.67%</span> <span class="c-amt">&#43;$15.00</span>`, // since bought
		`&#43;$5.00 (0.00%)</span> <span class="text-muted">today`,                             // whole portfolio
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	for _, unwanted := range []string{"Starting Balance", "export/transactions"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("page still has %q", unwanted)
		}
	}
}

// TestHistoryListsInterest checks a player's history has the interest paid on
// their cash in date order with their trades, leaving out payments that
// rounded to nothing.
func TestHistoryListsInterest(t *testing.T) {
	srv, q, d := gameServerDB(t)
	p, _ := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	for _, stmt := range []string{
		`INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total, created_at)
		 VALUES (?1, 1, 'buy', 10, 1000, 10000, 10000, '2026-10-29 15:00:00')`,
		`INSERT INTO interest_payments (participant_id, date, balance, rate, amount, created_at)
		 VALUES (?1, '2026-10-29', 100000000, 500, 13699, '2026-10-29 13:01:00'),
		        (?1, '2026-10-30', 100000000, 500, 13699, '2026-10-30 13:01:00'),
		        (?1, '2026-10-28', 1, 500, 0, '2026-10-28 13:01:00')`,
	} {
		if _, err := d.Exec(stmt, p.ID); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}

	body := getPage(t, srv, "/games/1/history", "")
	newest := strings.Index(body, "for 30 Oct 2026")
	trade := strings.Index(body, "<strong>ACME</strong>")
	oldest := strings.Index(body, "for 29 Oct 2026")
	if newest < 0 || trade < 0 || oldest < 0 {
		t.Fatalf("history is missing interest or the trade:\n%s", body)
	}
	if !(newest < trade && trade < oldest) {
		t.Error("history isn't newest first")
	}
	if !strings.Contains(body, "+$136.99") {
		t.Error("interest amount missing")
	}
	if strings.Contains(body, "28 Oct 2026") {
		t.Error("a payment that rounded to nothing is listed")
	}
}

func TestGameHomeIsRulesForNonPlayers(t *testing.T) {
	srv, _ := gameServer(t)
	body := getPage(t, srv, "/games/1", "2")
	if !strings.Contains(body, `href="/games/1" class="active">Overview`) {
		t.Error("Overview tab isn't active for a non-player")
	}
	if !strings.Contains(body, "Starting Balance") || !strings.Contains(body, "Join Game") {
		t.Error("non-player isn't shown the rules and a Join button")
	}
}

func TestRulesPage(t *testing.T) {
	srv, _ := gameServer(t)
	body := getPage(t, srv, "/games/1/rules", "")
	if !strings.Contains(body, `href="/games/1/rules" class="active">Rules`) || !strings.Contains(body, "Starting Balance") {
		t.Error("rules page doesn't show the rules under the Rules tab")
	}
}

func TestPortfolioRedirectsHome(t *testing.T) {
	srv, _ := gameServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/games/1/portfolio", nil))
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != "/games/1" {
		t.Fatalf("got %d to %q, want 301 to /games/1", w.Code, w.Header().Get("Location"))
	}
}

func TestRankPlayer(t *testing.T) {
	board := []db.ListParticipantsByGameRow{
		{ID: 1, PortfolioValue: 300, UserName: "Ann"},
		{ID: 2, PortfolioValue: 200, UserName: "Ben"},
		{ID: 3, PortfolioValue: 100, UserName: "Cat"},
	}
	for _, tc := range []struct {
		p    db.Participant
		want standing
	}{
		{db.Participant{ID: 1, PortfolioValue: 300}, standing{Rank: 1, Of: 3}},
		{db.Participant{ID: 3, PortfolioValue: 100}, standing{Rank: 3, Of: 3, Ahead: "Ben", Gap: 100}},
		// Private players aren't on the board but still get a spot.
		{db.Participant{ID: 9, PortfolioValue: 250}, standing{Rank: 2, Of: 4, Ahead: "Ann", Gap: 50}},
	} {
		if got := rankPlayer(tc.p, board); got != tc.want {
			t.Errorf("rankPlayer(%d) = %+v, want %+v", tc.p.ID, got, tc.want)
		}
	}
	for rank, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th", 21: "21st", 112: "112th"} {
		if got := (standing{Rank: rank}).Place(); got != want {
			t.Errorf("Place(%d) = %q, want %q", rank, got, want)
		}
	}
}

func TestChangePct(t *testing.T) {
	for _, tc := range []struct {
		c    change
		want string
	}{
		{change{50, 1000}, "+5.00%"},
		{change{-50, 1000}, "-5.00%"},
		{change{-1, 1000000}, "0.00%"}, // not "-0.00%"
		{change{5, 0}, ""},
		{change{1000, -10000}, "+10.00%"}, // a short that's up
		{change{-1000, -10000}, "-10.00%"},
	} {
		if got := tc.c.Pct(); got != tc.want {
			t.Errorf("%+v.Pct() = %q, want %q", tc.c, got, tc.want)
		}
	}
}

// TestDayMove: only moves after the player held the shares count. The
// previous close was $28.04 on 29 October and the latest is $28.64.
func TestDayMove(t *testing.T) {
	bars := []db.StockPrice{{Date: "2026-10-30", Close: 2864}, {Date: "2026-10-29", Close: 2804}}
	trade := func(at, side string, n, price int64) db.ListTradesSinceRow {
		return db.ListTradesSinceRow{CreatedAt: at, Type: side, Quantity: n, Price: price, Total: n * price}
	}
	for _, tc := range []struct {
		name   string
		qty    int64
		trades []db.ListTradesSinceRow
		want   change
	}{
		{"held overnight", 100, nil, change{6000, 280400}},
		{"bought before the previous close", 100,
			[]db.ListTradesSinceRow{trade("2026-10-29 15:00:00", "buy", 100, 2790)}, change{6000, 280400}},
		{"bought after hours at the latest close", 100,
			[]db.ListTradesSinceRow{trade("2026-10-30 22:30:00", "buy", 100, 2864)}, change{0, 286400}},
		{"bought during the session", 100,
			[]db.ListTradesSinceRow{trade("2026-10-30 15:00:00", "buy", 100, 2850)}, change{1400, 285000}},
		{"added to overnight shares", 150,
			[]db.ListTradesSinceRow{trade("2026-10-30 15:00:00", "buy", 50, 2850)}, change{6700, 422900}},
		{"sold half during the session", 50,
			[]db.ListTradesSinceRow{trade("2026-10-30 15:00:00", "sell", 50, 2850)}, change{5300, 137900}},
		{"sold out during the session", 0,
			[]db.ListTradesSinceRow{trade("2026-10-30 15:00:00", "sell", 100, 2850)}, change{4600, -4600}},
		{"shorted during the session", -100,
			[]db.ListTradesSinceRow{trade("2026-10-30 15:00:00", "sell", 100, 2900)}, change{3600, -290000}},
	} {
		if got := dayMove(tc.qty, bars, tc.trades); got != tc.want {
			t.Errorf("%s: dayMove = %+v, want %+v", tc.name, got, tc.want)
		}
	}
	if got := dayMove(100, bars[:1], nil); got != (change{}) {
		t.Errorf("one bar: dayMove = %+v, want no change", got)
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
		`href="/games/1/stocks" data-stocks-tab class="active">Stocks`,
		`href="/games/1/orders"`,
		`id="ticket-form"`,
		`hx-post="/games/1/trade/1"`, // trades in place, adding no history
		`<option value="limit">`,
		`id="side-sell" disabled`, // holds nothing, no shorting
		`class="trade-bar"`,       // the phone's Buy and Sell
		`data-ticket-open="sell" disabled`,
		`<a href="/games/1/stocks" class="back-link" data-back>&lsaquo; Stocks</a>`,
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
	if strings.Contains(body, `id="ticket-form"`) || strings.Contains(body, `class="trade-bar"`) {
		t.Error("non-member was shown the order ticket")
	}
	if !strings.Contains(body, "Join it") {
		t.Error("non-member wasn't told to join")
	}
	if user, _ := q.GetUser(t.Context(), 2); user.LastGameID.Valid {
		t.Error("viewing a game you haven't joined made it your current game")
	}
}

// setMarketOpen opens or closes the US market, for one test.
func setMarketOpen(t *testing.T, open bool) {
	old := marketClosed
	marketClosed = func(string) bool { return !open }
	t.Cleanup(func() { marketClosed = old })
}

// noAfterHours turns off after-hours trading in gameServer's game, so market
// orders placed while the market is closed queue for the open.
func noAfterHours(t *testing.T, q *db.Queries) {
	t.Helper()
	g, err := q.GetGame(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.UpdateGameSettings(t.Context(), db.UpdateGameSettingsParams{
		ID: g.ID, Name: g.Name, Description: g.Description, Markets: g.Markets,
		StartingBalance: g.StartingBalance, BaseCurrency: g.BaseCurrency, MaxParticipants: g.MaxParticipants,
		TradeFee: g.TradeFee, AllowShort: g.AllowShort, LimitOrders: g.LimitOrders,
		ReferralBonusPct: g.ReferralBonusPct, CreditInterestRate: g.CreditInterestRate,
	}); err != nil {
		t.Fatal(err)
	}
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
	noAfterHours(t, q)
	setMarketOpen(t, false)
	setMarketNow(t, nyTime(t, "2026-11-02 04:00"))
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
	if !strings.Contains(w.Header().Get("Set-Cookie"), "fills when it opens in 5 hr 30 min, at 1:30 am AEDT tomorrow.") {
		t.Errorf("flash doesn't say when the market opens: %q", w.Header().Get("Set-Cookie"))
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

// TestMarketOrderFillsAfterHours: by default a market order placed while the
// market is closed fills straight away at the last close.
func TestMarketOrderFillsAfterHours(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, false)
	w := postTicket(srv, url.Values{"side": {"buy"}, "order_type": {"market"}, "quantity": {"5"}})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/games/1/stocks/1" {
		t.Fatalf("got %d to %q, want 303 to /games/1/stocks/1", w.Code, w.Header().Get("Location"))
	}
	p, _ := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	if h, _ := q.GetHolding(t.Context(), db.GetHoldingParams{ParticipantID: p.ID, StockID: 1}); h.Quantity != 5 || h.CostBasis != 5*1050 {
		t.Errorf("holding = %d costing %d, want 5 costing %d", h.Quantity, h.CostBasis, 5*1050)
	}
	if orders, _ := q.ListOpenLimitOrders(t.Context(), p.ID); len(orders) != 0 {
		t.Errorf("open orders = %+v, want none", orders)
	}
}

func TestTicketSaysWhenMarketClosed(t *testing.T) {
	for _, open := range []bool{false, true} {
		srv, _ := gameServer(t)
		setMarketOpen(t, open)
		setMarketNow(t, nyTime(t, "2026-11-02 04:00"))
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, httptest.NewRequest("GET", "/games/1/stocks/1", nil))
		body := w.Body.String()
		if got := strings.Contains(body, `id="closed-note"`); got == open {
			t.Errorf("market open %v: closed note shown = %v", open, got)
		}
		if got := strings.Contains(body, "It opens in 5 hr 30 min, at 1:30 am AEDT tomorrow."); got == open {
			t.Errorf("market open %v: says when it opens = %v", open, got)
		}
		if got := strings.Contains(body, "at the last close"); got == open {
			t.Errorf("market open %v: order type says last close = %v", open, got)
		}
		if strings.Contains(body, `id="queued-note"`) {
			t.Errorf("market open %v: queued note shown in an after-hours game", open)
		}
	}
}

func TestTicketSaysWhenOrdersQueue(t *testing.T) {
	for _, open := range []bool{false, true} {
		srv, q := gameServer(t)
		noAfterHours(t, q)
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
