package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/exploded/game/internal/db"
)

// alice is user 1's place in gameServer's game.
func alice(t *testing.T, q *db.Queries) db.Participant {
	t.Helper()
	p, err := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func hold(t *testing.T, q *db.Queries, qty, cost, value int64) {
	t.Helper()
	if err := q.UpsertHolding(t.Context(), db.UpsertHoldingParams{
		ParticipantID: alice(t, q).ID, StockID: 1, Quantity: qty,
		AvgCost: cost / qty, CostBasis: cost, CurrentValue: value,
	}); err != nil {
		t.Fatal(err)
	}
}

func holding(t *testing.T, q *db.Queries) db.Holding {
	t.Helper()
	h, _ := q.GetHolding(t.Context(), db.GetHoldingParams{ParticipantID: alice(t, q).ID, StockID: 1})
	return h
}

// TestBuyingAtTwoPricesKeepsExactCost: 3 ACME at $10.50 then 1 at $10.51
// cost $42.01. An average kept in whole cents would say $42.00.
func TestBuyingAtTwoPricesKeepsExactCost(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, true)
	postTicket(srv, url.Values{"side": {"buy"}, "quantity": {"3"}})
	if err := q.UpsertPrice(t.Context(), db.UpsertPriceParams{StockID: 1, Date: "2026-10-29", Open: 1051, High: 1051, Low: 1051, Close: 1051}); err != nil {
		t.Fatal(err)
	}
	postTicket(srv, url.Values{"side": {"buy"}, "quantity": {"1"}})

	if h := holding(t, q); h.Quantity != 4 || h.CostBasis != 4201 || h.AvgCost != 1050 {
		t.Errorf("holding = %d costing %d at %d each, want 4 costing 4201 at 1050", h.Quantity, h.CostBasis, h.AvgCost)
	}
	// Worth 4 x $10.51 = $42.04: up 3c on $42.01.
	body := getPage(t, srv, "/games/1", "")
	if want := `<span class="pill pill-up">&#43;0.07%</span> <span class="c-amt">&#43;$0.03</span>`; !strings.Contains(body, want) {
		t.Errorf("home page is missing the total return %q", want)
	}
}

// TestShortReturnHasTheRightSign: 10 sold short at $10.00, now $9.00, is up
// 10%, not down.
func TestShortReturnHasTheRightSign(t *testing.T) {
	srv, q := gameServer(t)
	hold(t, q, -10, -10000, -9000)
	body := getPage(t, srv, "/games/1", "")
	if want := `<span class="pill pill-up">&#43;10.00%</span> <span class="c-amt">&#43;$10.00</span>`; !strings.Contains(body, want) {
		t.Errorf("home page is missing %q", want)
	}
	if !strings.Contains(body, `data-confirm="Buy back all 10 ACME at the market price to close your short?"`) {
		t.Error("a short has no Close position button that buys it back")
	}
}

func sellAll(srv http.Handler) (code int, location, flash string) {
	w := postTicket(srv, url.Values{"side": {"sell"}, "all": {"1"}, "from": {"home"}})
	return w.Code, w.Header().Get("Location"), w.Header().Get("Set-Cookie")
}

func TestSellAllFromHome(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, true)
	hold(t, q, 7, 6300, 7350)
	if body := getPage(t, srv, "/games/1", ""); !strings.Contains(body, `data-confirm="Sell all 7 ACME at the market price?"`) {
		t.Error("home page has no Sell button for ACME")
	}

	if code, loc, _ := sellAll(srv); code != http.StatusSeeOther || loc != "/games/1" {
		t.Fatalf("got %d to %q, want 303 to /games/1", code, loc)
	}
	if h := holding(t, q); h.ID != 0 {
		t.Errorf("still holding %d ACME", h.Quantity)
	}
	if p := alice(t, q); p.CashBalance != 100000000+7*1050 {
		t.Errorf("cash = %d, want %d", p.CashBalance, 100000000+7*1050)
	}

	// A second tap finds nothing to sell.
	if _, loc, flash := sellAll(srv); loc != "/games/1" || !strings.Contains(flash, "hold any ACME") {
		t.Errorf("second tap went to %q with %q, want home with an error", loc, flash)
	}
	if h := holding(t, q); h.ID != 0 {
		t.Errorf("second tap left %d ACME", h.Quantity)
	}
}

func TestSellAllDoesntCoverOrDeepenAShort(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, true)
	hold(t, q, -3, -3000, -3150)
	if _, _, flash := sellAll(srv); !strings.Contains(flash, "hold any ACME") {
		t.Errorf("flash = %q, want an error", flash)
	}
	if h := holding(t, q); h.Quantity != -3 {
		t.Errorf("holding = %d, want -3", h.Quantity)
	}
}

func TestSellAllQueuesOnceWhileClosed(t *testing.T) {
	srv, q := gameServer(t)
	noAfterHours(t, q)
	setMarketOpen(t, false)
	hold(t, q, 7, 6300, 7350)

	if code, loc, _ := sellAll(srv); code != http.StatusSeeOther || loc != "/games/1" {
		t.Fatalf("got %d to %q, want 303 to /games/1", code, loc)
	}
	if _, _, flash := sellAll(srv); !strings.Contains(flash, "already queued") {
		t.Errorf("second tap flash = %q, want already queued", flash)
	}
	orders, _ := q.ListOpenLimitOrders(t.Context(), alice(t, q).ID)
	if len(orders) != 1 || orders[0].Type != "sell" || orders[0].Quantity != 7 {
		t.Errorf("open orders = %+v, want one queued sale of 7", orders)
	}
	if h := holding(t, q); h.Quantity != 7 {
		t.Errorf("holding = %d before the open, want 7", h.Quantity)
	}
	if body := getPage(t, srv, "/games/1", ""); !strings.Contains(body, "Close queued") || strings.Contains(body, `name="all"`) {
		t.Error("home page doesn't show the queued sale in place of the Close position button")
	}
}

func coverAll(srv http.Handler) (code int, location, flash string) {
	w := postTicket(srv, url.Values{"side": {"buy"}, "all": {"1"}, "from": {"home"}})
	return w.Code, w.Header().Get("Location"), w.Header().Get("Set-Cookie")
}

// TestCloseShortFromHome: 3 ACME short, bought back at $10.50.
func TestCloseShortFromHome(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, true)
	hold(t, q, -3, -3000, -3150)

	if code, loc, _ := coverAll(srv); code != http.StatusSeeOther || loc != "/games/1" {
		t.Fatalf("got %d to %q, want 303 to /games/1", code, loc)
	}
	if h := holding(t, q); h.ID != 0 {
		t.Errorf("still holding %d ACME", h.Quantity)
	}
	if p := alice(t, q); p.CashBalance != 100000000-3*1050 {
		t.Errorf("cash = %d, want %d", p.CashBalance, 100000000-3*1050)
	}

	// A second tap finds nothing to buy back, and doesn't go long.
	if _, loc, flash := coverAll(srv); loc != "/games/1" || !strings.Contains(flash, "short ACME") {
		t.Errorf("second tap went to %q with %q, want home with an error", loc, flash)
	}
	if h := holding(t, q); h.ID != 0 {
		t.Errorf("second tap left %d ACME", h.Quantity)
	}
}

func TestCoverAllDoesntBuyMoreOfALong(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, true)
	hold(t, q, 7, 6300, 7350)
	if _, _, flash := coverAll(srv); !strings.Contains(flash, "short ACME") {
		t.Errorf("flash = %q, want an error", flash)
	}
	if h := holding(t, q); h.Quantity != 7 {
		t.Errorf("holding = %d, want 7", h.Quantity)
	}
}

// TestFinishedGameIsFrozen: 10 ACME were worth $10.00 each when the game
// ended. ACME has moved since, but the home page still shows the end.
func TestFinishedGameIsFrozen(t *testing.T) {
	srv, q := gameServer(t)
	hold(t, q, 10, 9000, 10000)
	if _, err := q.FinishExpiredGames(t.Context(), "2027-01-01"); err != nil {
		t.Fatal(err)
	}

	body := getPage(t, srv, "/games/1", "")
	if !strings.Contains(body, `data-label="Price">$10.00</td>`) {
		t.Error("price isn't the one from the end of the game")
	}
	if strings.Contains(body, `data-label="Today"`) || strings.Contains(body, "Close position") {
		t.Error("a finished game shows today's move or a Close position button")
	}
	if !strings.Contains(body, "Final value") {
		t.Error("a finished game's value isn't labelled final")
	}
}

// TestCurrencyFee: in an AUD game, 100 ACME at US$10.50 is A$1,615.38 at
// 0.65, plus the default 0.5% currency fee, A$8.08, both ways.
func TestCurrencyFee(t *testing.T) {
	srv, q, d := gameServerDB(t)
	setMarketOpen(t, true)
	for _, stmt := range []string{
		`UPDATE games SET base_currency = 'AUD' WHERE id = 1`,
		`INSERT INTO exchange_rates (date, rate_aud_usd) VALUES ('2026-10-28', 650000)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if body := getPage(t, srv, "/games/1/stocks/1", ""); !strings.Contains(body, `data-fx-fee="50"`) || !strings.Contains(body, "Currency fee (0.50%)") {
		t.Error("ticket doesn't show the currency fee")
	}

	postTicket(srv, url.Values{"side": {"buy"}, "quantity": {"100"}})
	if p := alice(t, q); p.CashBalance != 100000000-161538-808 {
		t.Errorf("cash after buying = %d, want %d", p.CashBalance, 100000000-161538-808)
	}
	postTicket(srv, url.Values{"side": {"sell"}, "quantity": {"100"}})
	if p := alice(t, q); p.CashBalance != 100000000-2*808 {
		t.Errorf("cash after selling = %d, want %d", p.CashBalance, 100000000-2*808)
	}
	var fees int64
	if err := d.QueryRow(`SELECT SUM(fee) FROM transactions`).Scan(&fees); err != nil || fees != 2*808 {
		t.Errorf("fees recorded = %d (%v), want %d", fees, err, 2*808)
	}
}

// postTicketHTMX posts the stock page's order ticket the way htmx does.
func postTicketHTMX(srv http.Handler, form url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/games/1/trade/1", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w
}

// TestTicketTradesInPlace: a buy from the ticket redraws the ticket with a
// confirmation and a way back to the list, and updates the game bar, rather
// than redirecting to the same page and adding to the browser's history.
func TestTicketTradesInPlace(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, true)
	w := postTicketHTMX(srv, url.Values{"side": {"buy"}, "quantity": {"5"}, "back": {"/games/1/stocks?f=tech"}})
	if w.Code != http.StatusOK || w.Header().Get("Location") != "" {
		t.Fatalf("got %d to %q, want 200 and no redirect", w.Code, w.Header().Get("Location"))
	}
	body := w.Body.String()
	for _, want := range []string{
		`class="ticket ticket-sheet is-done"`,
		`Bought 5 shares of ACME`,
		`<a href="/games/1/stocks?f=tech" class="btn btn-primary" data-back>&lsaquo; Back to Stocks</a>`,
		`View your portfolio`,
		`data-held="5"`,
		`<input type="hidden" name="back" value="/games/1/stocks?f=tech">`,
		`id="game-bar-stats" hx-swap-oob="true"`,
		`$999,947.50`, // the game bar's cash after 5 x $10.50
	} {
		if !strings.Contains(body, want) {
			t.Errorf("reply is missing %q", want)
		}
	}
	if strings.Contains(body, "<html") {
		t.Error("reply is a whole page, not the ticket")
	}
	if h := holding(t, q); h.Quantity != 5 {
		t.Errorf("holding = %d, want 5", h.Quantity)
	}
}

func TestTicketRefusalIsAToast(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, true)
	w := postTicketHTMX(srv, url.Values{"side": {"buy"}, "quantity": {"100000000"}})
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 so the ticket stays as it was", w.Code)
	}
	if trig := w.Header().Get("HX-Trigger"); !strings.Contains(trig, "Insufficient funds") || !strings.Contains(trig, `"error"`) {
		t.Errorf("HX-Trigger = %q, want an error toast", trig)
	}
	if p := alice(t, q); p.CashBalance != 100000000 {
		t.Errorf("cash = %d, want it unchanged", p.CashBalance)
	}
}

// TestTicketOrdersStayOnTheStock: limit and queued orders used to send the
// player to the Orders tab; from the ticket they stay and link to it.
func TestTicketOrdersStayOnTheStock(t *testing.T) {
	srv, q := gameServer(t)
	setMarketOpen(t, true)
	w := postTicketHTMX(srv, url.Values{"side": {"buy"}, "order_type": {"limit"}, "quantity": {"2"}, "limit_price": {"9.00"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Limit buy order placed for 2 shares of ACME") ||
		!strings.Contains(w.Body.String(), `href="/games/1/orders" class="ticket-done-link"`) {
		t.Errorf("limit order: got %d, body without the confirmation or Orders link", w.Code)
	}

	noAfterHours(t, q)
	setMarketOpen(t, false)
	w = postTicketHTMX(srv, url.Values{"side": {"buy"}, "order_type": {"market"}, "quantity": {"3"}})
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Order queued: buy 3 shares of ACME") {
		t.Errorf("queued order: got %d, body without the confirmation", w.Code)
	}
	if orders, _ := q.ListOpenLimitOrders(t.Context(), alice(t, q).ID); len(orders) != 2 {
		t.Errorf("%d open orders, want 2", len(orders))
	}
}

func TestBackLink(t *testing.T) {
	for _, c := range []struct{ ref, url, label string }{
		{"http://example.com/games/1/stocks?f=tech&q=", "/games/1/stocks?f=tech&q=", "Stocks"},
		{"/games/1/stocks", "/games/1/stocks", "Stocks"},
		{"http://example.com/games/1", "/games/1", "Portfolio"},
		{"http://example.com/games/1/watchlist", "/games/1/watchlist", "Watchlist"},
		{"http://example.com/games/1/orders", "/games/1/orders", "Orders"},
		{"http://example.com/games/1/stocks/2", "/games/1/stocks/2", "Back"}, // a peer
		{"http://example.com/games/1/stocks/1", "/games/1/stocks", "Stocks"}, // this stock
		{"http://example.com/games/12/stocks", "/games/1/stocks", "Stocks"},  // another game
		{"http://example.com/learn/v/7?t=90", "/learn/v/7?t=90", "Video"},
		{"http://evil.example/games/1/stocks", "/games/1/stocks", "Stocks"},
		{"//evil.example/games/1", "/games/1/stocks", "Stocks"},
		{"javascript:alert(1)", "/games/1/stocks", "Stocks"},
		{"", "/games/1/stocks", "Stocks"},
	} {
		if got := backLink(c.ref, "example.com", 1, 1); got.URL != c.url || got.Label != c.label {
			t.Errorf("backLink(%q) = %+v, want %s %q", c.ref, got, c.label, c.url)
		}
	}
}
