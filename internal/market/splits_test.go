package market

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/exploded/game/internal/db"
)

// splitsDB seeds a running USD game with three players and two stocks, and
// returns a fetcher whose splits calendar says NVDA split 10-for-1 and REV
// had a 1-for-4 reverse split on Wednesday 28 October 2026. The split took
// effect at 13:30 UTC that day.
func splitsDB(t *testing.T) (*sql.DB, *db.Queries, *Fetcher) {
	t.Helper()
	if NewYork == nil {
		t.Skip("no America/New_York timezone")
	}
	d, q := monthlyDB(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x', 'A', 't1'), (2, 'b@x', 'B', 't2'), (3, 'c@x', 'C', 't3')`,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date)
			VALUES (1, 1, 'October', 'active', 'USD', '2026-10-01', '2026-10-31')`,
		`INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES
			(1, 1, 1, 0, 0), (2, 1, 2, 0, 0), (3, 1, 3, 0, 0)`,
		`INSERT INTO stocks (id, symbol, name, market, currency) VALUES
			(1, 'NVDA', 'NVIDIA', 'sp500', 'USD'), (2, 'REV', 'Reverse Co', 'sp500', 'USD')`,
		// NVDA bars: two fetched before the split at $1,200, one refetched
		// after it (already split by Twelve Data), and the split day's quote.
		`INSERT INTO stock_prices (stock_id, date, open, high, low, close, volume, created_at) VALUES
			(1, '2026-10-26', 120000, 120000, 120000, 120000, 100, '2026-10-26 22:05:00'),
			(1, '2026-10-27', 120000, 120000, 120000, 120000, 100, '2026-10-27 22:05:00'),
			(1, '2026-10-20', 11800, 11800, 11800, 11800, 1000, '2026-10-30 08:00:00'),
			(1, '2026-10-28', 12100, 12100, 12100, 12100, 1000, '2026-10-28 15:00:00'),
			(2, '2026-10-27', 100, 100, 100, 100, 400, '2026-10-27 22:05:00')`,
		// Player 1 held 5 NVDA from before the split. Player 2 bought 3 after
		// the open, already split. Player 3 held 2 and bought 10 after.
		`INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost, cost_basis) VALUES
			(1, 1, 5, 120000, 600000), (2, 1, 3, 12100, 36300), (3, 1, 12, 30167, 362000),
			(1, 2, 10, 4000, 40000)`,
		`INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total, created_at) VALUES
			(1, 1, 'buy', 5, 120000, 600000, 600000, '2026-10-20 15:00:00'),
			(2, 1, 'buy', 3, 12100, 36300, 36300, '2026-10-28 15:00:00'),
			(3, 1, 'buy', 2, 120000, 240000, 240000, '2026-10-21 15:00:00'),
			(3, 1, 'buy', 10, 12200, 122000, 122000, '2026-10-28 16:00:00'),
			(1, 2, 'buy', 10, 4000, 40000, 40000, '2026-10-21 15:00:00')`,
		// An order from before the split and one from after.
		`INSERT INTO limit_orders (id, participant_id, stock_id, type, quantity, limit_price, created_at) VALUES
			(1, 1, 1, 'buy', 2, 110000, '2026-10-27 01:00:00'),
			(2, 2, 1, 'buy', 5, 11500, '2026-10-28 18:00:00')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	body := `[
		{"date":"2026-10-28","symbol":"NVDA","exchange":"NASDAQ","description":"10-for-1 split","ratio":0.1,"from_factor":10,"to_factor":1},
		{"date":"2026-10-28","symbol":"REV","exchange":"NYSE","description":"1-for-4 split","ratio":4,"from_factor":1,"to_factor":4},
		{"date":"2026-10-27","symbol":"ZZZZ","exchange":"OTC","description":"1-for-10 split","ratio":10,"from_factor":1,"to_factor":10}
	]`
	return d, q, &Fetcher{q: q, rawDB: d, client: &http.Client{Transport: twelveDataStub(body)}}
}

// TestCheckSplitsAppliedLate: the check ran the day after the split, so
// only what came before the split's open is scaled, and nothing twice.
func TestCheckSplitsAppliedLate(t *testing.T) {
	d, q, f := splitsDB(t)
	ctx := context.Background()

	n, err := f.CheckSplits(ctx, q, "2026-10-29")
	if err != nil || n != 2 {
		t.Fatalf("CheckSplits: %d applied, %v; want 2", n, err)
	}
	if n, err := f.CheckSplits(ctx, q, "2026-10-29"); err != nil || n != 0 {
		t.Fatalf("second check: %d applied, %v; want 0", n, err)
	}

	holding := func(p, s int64) (qty, cost int64) {
		d.QueryRow(`SELECT quantity, cost_basis FROM holdings WHERE participant_id = ? AND stock_id = ?`, p, s).Scan(&qty, &cost)
		return
	}
	for _, c := range []struct {
		player, wantQty, wantCost int64
	}{
		{1, 50, 600000}, // 5 held before: 50, same cost
		{2, 3, 36300},   // bought after the open: already split
		{3, 30, 362000}, // 2 held before become 20, plus 10 bought after
	} {
		if qty, cost := holding(c.player, 1); qty != c.wantQty || cost != c.wantCost {
			t.Errorf("player %d NVDA: %d shares costing %d, want %d costing %d", c.player, qty, cost, c.wantQty, c.wantCost)
		}
	}

	closeOn := func(stock int64, date string) (close int64) {
		d.QueryRow(`SELECT close FROM stock_prices WHERE stock_id = ? AND date = ?`, stock, date).Scan(&close)
		return
	}
	for date, want := range map[string]int64{
		"2026-10-27": 12000, // fetched before: divided by 10
		"2026-10-20": 11800, // refetched after: already split
		"2026-10-28": 12100, // the split day's own bar
	} {
		if got := closeOn(1, date); got != want {
			t.Errorf("NVDA close on %s: %d, want %d", date, got, want)
		}
	}

	var qty, price int64
	d.QueryRow(`SELECT quantity, limit_price FROM limit_orders WHERE id = 1`).Scan(&qty, &price)
	if qty != 20 || price != 11000 {
		t.Errorf("order placed before the split: %d @ %d, want 20 @ 11000", qty, price)
	}
	d.QueryRow(`SELECT quantity, limit_price FROM limit_orders WHERE id = 2`).Scan(&qty, &price)
	if qty != 5 || price != 11500 {
		t.Errorf("order placed after the split changed: %d @ %d", qty, price)
	}

	// REV 1-for-4: 10 shares are 2.5, so 2 shares and half a share in cash
	// at the split price ($4.00), taking a fifth of the cost with it.
	if qty, cost := holding(1, 2); qty != 2 || cost != 32000 {
		t.Errorf("REV: %d shares costing %d, want 2 costing 32000", qty, cost)
	}
	var cash int64
	d.QueryRow(`SELECT cash_balance FROM participants WHERE id = 1`).Scan(&cash)
	if cash != 200 {
		t.Errorf("player 1 cash %d, want 200 for half a REV share", cash)
	}

	var notes int
	d.QueryRow(`SELECT COUNT(*) FROM notifications WHERE type = 'stock_split'`).Scan(&notes)
	if notes != 3 {
		t.Errorf("%d split notifications, want 3 (two NVDA holders from before, one REV)", notes)
	}
}

func TestSplitShares(t *testing.T) {
	for _, c := range []struct {
		n      int64
		shares float64
		whole  int64
		frac   float64
	}{
		{5, 10, 50, 0},
		{10, 0.25, 2, 0.5},
		{-10, 0.25, -2, -0.5}, // a short stays short
		{3, 1.5, 4, 0.5},
	} {
		whole, frac := splitShares(c.n, c.shares)
		if whole != c.whole || frac < c.frac-1e-9 || frac > c.frac+1e-9 {
			t.Errorf("splitShares(%d, %v) = %d, %v; want %d, %v", c.n, c.shares, whole, frac, c.whole, c.frac)
		}
	}
}
