package handler

import (
	"net/http"
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
	if strings.Contains(body, `name="all"`) {
		t.Error("a short has a Sell all button")
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
	if body := getPage(t, srv, "/games/1", ""); !strings.Contains(body, "Sale queued") || strings.Contains(body, `name="all"`) {
		t.Error("home page doesn't show the queued sale in place of the Sell button")
	}
}
