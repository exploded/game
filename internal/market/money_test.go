package market

import (
	"context"
	"net/http"
	"testing"

	"github.com/exploded/game/internal/db"
)

// Tests for the money fixes from the 19 November 2026 review.

func cashOf(t *testing.T, q *db.Queries) int64 {
	t.Helper()
	p, err := q.GetParticipant(t.Context(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return p.CashBalance
}

// TestCancelledOrderDoesNotFill: the player cancels after ProcessLimitOrders
// has listed the order but before it fills. The cancel wins: no shares, no
// cash taken.
func TestCancelledOrderDoesNotFill(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "buy", 5)
	setMarketOpen(t, true)
	orders, err := q.ListAllOpenOrders(t.Context())
	if err != nil || len(orders) != 1 {
		t.Fatalf("open orders = %d, %v; want 1", len(orders), err)
	}
	if n, err := q.CancelOpenOrder(t.Context(), orders[0].ID); err != nil || n != 1 {
		t.Fatalf("cancel: %d, %v", n, err)
	}

	filled, err := fillLimitOrder(t.Context(), q, d, orders[0], 1050)
	if err != nil || filled {
		t.Fatalf("fill after cancel = %v, %v; want false, nil", filled, err)
	}
	if s := orderStatus(t, d); s != "cancelled" {
		t.Errorf("status = %q, want cancelled", s)
	}
	if c := cashOf(t, q); c != 100000 {
		t.Errorf("cash = %d, want 100000 untouched", c)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM transactions`).Scan(&n)
	if n != 0 {
		t.Errorf("%d transactions, want 0", n)
	}
}

// TestFillRollsBackWithoutRate: a USD order in an AUD game with no rate
// stored stays open, with nothing written, for the next run.
func TestFillRollsBackWithoutRate(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "buy", 5)
	if _, err := d.Exec(`UPDATE games SET base_currency = 'AUD'`); err != nil {
		t.Fatal(err)
	}
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q, d)
	if s := orderStatus(t, d); s != "open" {
		t.Errorf("status = %q, want open (rolled back)", s)
	}
	if c := cashOf(t, q); c != 100000 {
		t.Errorf("cash = %d, want 100000", c)
	}
}

// TestLimitFillChargesCurrencyFee: 2 ACME at US$10.50 in an AUD game at 0.70
// is A$30.00, plus the default 0.5% currency fee, A$0.15.
func TestLimitFillChargesCurrencyFee(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "buy", 5)
	for _, stmt := range []string{
		`UPDATE games SET base_currency = 'AUD'`,
		`DELETE FROM limit_orders`,
		`INSERT INTO exchange_rates (date, rate_aud_usd) VALUES ('2026-10-28', 700000)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	id := limitOrder(t, q, "buy", 1100)
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q, d)
	if s := statusOf(t, d, id); s != "filled" {
		t.Fatalf("status = %q, want filled", s)
	}
	if c := cashOf(t, q); c != 100000-3000-15 {
		t.Errorf("cash = %d, want %d", c, 100000-3000-15)
	}
	var fee, converted int64
	d.QueryRow(`SELECT fee, converted_total FROM transactions`).Scan(&fee, &converted)
	if fee != 15 || converted != 3000 {
		t.Errorf("transaction fee %d on %d, want 15 on 3000", fee, converted)
	}
}

// TestFillCancelsRetiredStock: an order in a stock retired since it was
// placed is cancelled, not filled at the frozen price.
func TestFillCancelsRetiredStock(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "buy", 5)
	if _, err := d.Exec(`UPDATE stocks SET is_active = 0`); err != nil {
		t.Fatal(err)
	}
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q, d)
	if s := orderStatus(t, d); s != "cancelled" {
		t.Errorf("status = %q, want cancelled", s)
	}
	if c := cashOf(t, q); c != 100000 {
		t.Errorf("cash = %d, want 100000", c)
	}
}

// TestFillCancelsOversizeOrder: an order too big to value safely is
// cancelled, never converted with a wrapped total.
func TestFillCancelsOversizeOrder(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "buy", 5)
	for _, stmt := range []string{
		`UPDATE games SET base_currency = 'AUD'`,
		`UPDATE limit_orders SET quantity = 8800000000`,
		`INSERT INTO exchange_rates (date, rate_aud_usd) VALUES ('2026-10-28', 650000)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q, d)
	if s := orderStatus(t, d); s != "cancelled" {
		t.Errorf("status = %q, want cancelled", s)
	}
	if c := cashOf(t, q); c != 100000 {
		t.Errorf("cash = %d, want 100000", c)
	}
}

// TestQueuedShortIsCapped: a queued sell going short is cancelled when the
// short would be worth more than the portfolio ($1,050 short against $1,000).
func TestQueuedShortIsCapped(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "sell", 100)
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q, d)
	if s := orderStatus(t, d); s != "cancelled" {
		t.Errorf("status = %q, want cancelled", s)
	}
	if c := cashOf(t, q); c != 100000 {
		t.Errorf("cash = %d, want 100000", c)
	}
}

func TestTradeValue(t *testing.T) {
	for _, c := range []struct {
		qty, price int64
		ok         bool
	}{
		{5, 1050, true},
		{0, 1050, false},
		{-1, 1050, false},
		{5, 0, false},
		{8800000000, 1050, false}, // US$92 billion
		{952380952, 1050, true},   // just under US$10 billion
		{1, MaxTradeValue, true},  // the cap itself
		{2, MaxTradeValue/2 + 1, false},
	} {
		if _, ok := TradeValue(c.qty, c.price); ok != c.ok {
			t.Errorf("TradeValue(%d, %d) ok = %v, want %v", c.qty, c.price, ok, c.ok)
		}
	}
	// Under the cap, converting to AUD at any plausible rate can't wrap.
	v, _ := TradeValue(1, MaxTradeValue)
	if got := convertCurrency(v, "USD", "AUD", 100000); got <= 0 {
		t.Errorf("converting the cap gave %d", got)
	}
}

// TestExDividendClose: a close from the day before a $0.50 ex-date is worth
// $0.50 less; a close from the ex-date itself is left alone.
func TestExDividendClose(t *testing.T) {
	q, d := ordersDB(t, "2026-10-28", "buy", 5)
	if _, err := d.Exec(`INSERT INTO stock_dividends (stock_id, ex_date, per_share) VALUES (1, '2026-10-29', 500000)`); err != nil {
		t.Fatal(err)
	}
	if got := ExDividendClose(t.Context(), q, 1, db.StockPrice{Date: "2026-10-28", Close: 1050}); got != 1000 {
		t.Errorf("close before the ex-date = %d, want 1000", got)
	}
	if got := ExDividendClose(t.Context(), q, 1, db.StockPrice{Date: "2026-10-29", Close: 1000}); got != 1000 {
		t.Errorf("close on the ex-date = %d, want 1000 unchanged", got)
	}
	if got := ExDividendClose(t.Context(), q, 1, db.StockPrice{Date: "2026-10-28", Close: 30}); got != 1 {
		t.Errorf("tiny close = %d, want floored at 1", got)
	}
}

// TestRevalueAfterDividend: a $0.50 dividend paid before the ex-date's first
// quote leaves 10 shares valued at $10.00, not the $10.50 close, so the
// player isn't counted twice for it.
func TestRevalueAfterDividend(t *testing.T) {
	q, d := ordersDB(t, "2026-10-28", "buy", 5)
	for _, stmt := range []string{
		`INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost, cost_basis, current_value) VALUES (1, 1, 10, 1000, 10000, 10500)`,
		`INSERT INTO stock_dividends (stock_id, ex_date, per_share) VALUES (1, '2026-10-29', 500000)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := RevaluePortfolios(t.Context(), q, d); err != nil {
		t.Fatal(err)
	}
	p, _ := q.GetParticipant(t.Context(), 1)
	if p.PortfolioValue != 100000+10000 {
		t.Errorf("value = %d, want %d", p.PortfolioValue, 100000+10000)
	}
	h, _ := q.GetHolding(t.Context(), db.GetHoldingParams{ParticipantID: 1, StockID: 1})
	if h.CurrentValue != 10000 {
		t.Errorf("holding value = %d, want 10000", h.CurrentValue)
	}
}

// TestDividendPaidToSellerAfterOpen: a player held 50 MS at the ex-date's
// open and sold all of them later that day, before the late check ran. They
// held the shares at the open, so they're paid.
func TestDividendPaidToSellerAfterOpen(t *testing.T) {
	if NewYork == nil {
		t.Skip("no America/New_York timezone")
	}
	d, q := monthlyDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x', 'A', 't1'), (2, 'b@x', 'B', 't2')`,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date) VALUES
			(1, 1, 'USD game', 'active', 'USD', '2026-10-01', '2026-11-30')`,
		`INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 1, 0, 0), (2, 1, 2, 0, 0)`,
		`INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'MS', 'Morgan Stanley', 'sp500', 'USD')`,
		// Player 1 bought before the open and sold after it. Player 2
		// bought and sold before the open, so held nothing then.
		`INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total, created_at) VALUES
			(1, 1, 'buy', 50, 10000, 500000, 500000, '2026-10-29 15:00:00'),
			(1, 1, 'sell', 50, 10000, 500000, 500000, '2026-10-30 15:00:00'),
			(2, 1, 'buy', 50, 10000, 500000, 500000, '2026-10-29 15:00:00'),
			(2, 1, 'sell', 50, 10000, 500000, 500000, '2026-10-30 10:00:00')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	body := `[{"symbol":"MS","mic_code":"XNYS","exchange":"NYSE","ex_date":"2026-10-30","amount":1.15}]`
	f := &Fetcher{q: q, rawDB: d, client: &http.Client{Transport: twelveDataStub(body)}}
	if n, err := f.CheckDividends(context.Background(), q, "2026-11-02"); err != nil || n != 1 {
		t.Fatalf("CheckDividends: %d, %v", n, err)
	}
	for _, c := range []struct{ player, want int64 }{
		{1, 5750}, // 50 × $1.15
		{2, 0},
	} {
		var cash int64
		d.QueryRow(`SELECT cash_balance FROM participants WHERE id = ?`, c.player).Scan(&cash)
		if cash != c.want {
			t.Errorf("player %d cash %d, want %d", c.player, cash, c.want)
		}
	}
}
