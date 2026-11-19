package handler

import (
	"net/url"
	"testing"
)

// Tests for the money fixes from the 19 November 2026 review.

// TestHugeOrderIsRefused is the review's exploit: in an AUD game, buying 8.8
// billion ACME at US$10.50 (US$92 billion) wrapped int64 when converted to
// AUD, so the cost went negative and cash went up. It must be refused with
// nothing written.
func TestHugeOrderIsRefused(t *testing.T) {
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
	for _, qty := range []string{"8800000000", "9223372036854775807"} {
		postTicket(srv, url.Values{"side": {"buy"}, "quantity": {qty}})
		if p := alice(t, q); p.CashBalance != 100000000 {
			t.Fatalf("buying %s: cash = %d, want 100000000", qty, p.CashBalance)
		}
	}
	if h := holding(t, q); h.Quantity != 0 {
		t.Errorf("holding = %d, want none", h.Quantity)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&n)
	if n != 0 {
		t.Errorf("%d transactions, want 0", n)
	}
	// A limit order that size is refused too.
	postTicket(srv, url.Values{"side": {"buy"}, "order_type": {"limit"}, "quantity": {"8800000000"}, "limit_price": {"10.50"}})
	d.QueryRow(`SELECT COUNT(*) FROM limit_orders`).Scan(&n)
	if n != 0 {
		t.Errorf("%d limit orders, want 0", n)
	}
}

// TestRetiredStockCantBeTraded: a posted form can't buy a retired stock at
// its frozen last price.
func TestRetiredStockCantBeTraded(t *testing.T) {
	srv, q, d := gameServerDB(t)
	setMarketOpen(t, true)
	if _, err := d.Exec(`UPDATE stocks SET is_active = 0 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	postTicket(srv, url.Values{"side": {"buy"}, "quantity": {"5"}})
	if p := alice(t, q); p.CashBalance != 100000000 {
		t.Errorf("cash = %d, want 100000000", p.CashBalance)
	}
	if h := holding(t, q); h.Quantity != 0 {
		t.Errorf("holding = %d, want none", h.Quantity)
	}
}

// TestShortsCappedAtPortfolioValue: with shorting on, Alice ($1,000,000) can
// short $945,000 of ACME but not $1,050,000.
func TestShortsCappedAtPortfolioValue(t *testing.T) {
	srv, q, d := gameServerDB(t)
	setMarketOpen(t, true)
	if _, err := d.Exec(`UPDATE games SET allow_short = 1 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	postTicket(srv, url.Values{"side": {"sell"}, "quantity": {"100000"}})
	if h := holding(t, q); h.Quantity != 0 {
		t.Fatalf("short of $1,050,000 went through: holding %d", h.Quantity)
	}
	postTicket(srv, url.Values{"side": {"sell"}, "quantity": {"90000"}})
	if h := holding(t, q); h.Quantity != -90000 {
		t.Errorf("holding = %d, want -90000", h.Quantity)
	}
}

// TestAfterHoursFillTakesOffTodaysDividend: ACME paid $0.50 this morning
// (ex-date 29 October), but the newest close is still 28 October's $10.50.
// An after-hours buy fills at $10.00, so buying at the close, collecting the
// dividend and selling back isn't free money.
func TestAfterHoursFillTakesOffTodaysDividend(t *testing.T) {
	srv, q, d := gameServerDB(t)
	setMarketOpen(t, false) // after-hours trading is on by default
	if _, err := d.Exec(`INSERT INTO stock_dividends (stock_id, ex_date, per_share) VALUES (1, '2026-10-29', 500000)`); err != nil {
		t.Fatal(err)
	}
	postTicket(srv, url.Values{"side": {"buy"}, "quantity": {"10"}})
	if p := alice(t, q); p.CashBalance != 100000000-10000 {
		t.Errorf("cash = %d, want %d", p.CashBalance, 100000000-10000)
	}
	var price int64
	d.QueryRow(`SELECT price FROM transactions`).Scan(&price)
	if price != 1000 {
		t.Errorf("filled at %d, want 1000", price)
	}
}
