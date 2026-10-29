package market

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
	_ "modernc.org/sqlite"
)

// ordersDB seeds an active USD game with a player holding $1,000 cash, an
// S&P 500 stock (id 1) priced at $10.50 on priceDate, and one queued market
// order from the player.
func ordersDB(t *testing.T, priceDate, side string, quantity int64) (*db.Queries, *sql.DB) {
	t.Helper()
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(game.Schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	for _, stmt := range []string{
		`INSERT INTO users (id, google_id, email, name) VALUES (1, 'g1', 'a@x', 'Alice')`,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date, allow_short)
		 VALUES (1, 1, 'Spring Cup', 'active', 'USD', '2026-01-01', '2026-12-31', 1)`,
		`INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 1, 100000, 100000)`,
		`INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'ACME', 'Acme Corp', 'sp500', 'USD')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	if _, err := d.Exec(`INSERT INTO stock_prices (stock_id, date, open, high, low, close, volume) VALUES (1, ?, 1000, 1100, 900, 1050, 5000)`, priceDate); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	if _, err := q.CreateMarketOrder(t.Context(), db.CreateMarketOrderParams{
		ParticipantID: 1, StockID: 1, Type: side, Quantity: quantity,
	}); err != nil {
		t.Fatal(err)
	}
	return q, d
}

// setMarketOpen sets what intraday quotes last said about the session, and
// restores it after the test.
func setMarketOpen(t *testing.T, open bool) {
	t.Helper()
	quoteStatus.mu.Lock()
	oldAt, oldOpen := quoteStatus.at, quoteStatus.open
	quoteStatus.at, quoteStatus.open = time.Now(), open
	quoteStatus.mu.Unlock()
	t.Cleanup(func() {
		quoteStatus.mu.Lock()
		quoteStatus.at, quoteStatus.open = oldAt, oldOpen
		quoteStatus.mu.Unlock()
	})
}

func sessionToday(t *testing.T) string {
	t.Helper()
	if NewYork == nil {
		t.Skip("no timezone database")
	}
	return time.Now().In(NewYork).Format("2006-01-02")
}

func orderStatus(t *testing.T, d *sql.DB) string {
	t.Helper()
	var status string
	if err := d.QueryRow("SELECT status FROM limit_orders WHERE id = 1").Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

func notificationTitle(d *sql.DB) string {
	var title string
	d.QueryRow("SELECT title FROM notifications ORDER BY id DESC LIMIT 1").Scan(&title)
	return title
}

func TestQueuedMarketOrderWaitsWhileClosed(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "buy", 5)
	setMarketOpen(t, false)
	ProcessLimitOrders(t.Context(), q)
	if s := orderStatus(t, d); s != "open" {
		t.Errorf("status = %q while the market is closed, want open", s)
	}
}

// TestQueuedMarketOrderWaitsForTodaysPrice: the market has opened but this
// stock's newest price is still yesterday's close, so it must not fill at it.
func TestQueuedMarketOrderWaitsForTodaysPrice(t *testing.T) {
	sessionToday(t)
	q, d := ordersDB(t, "2020-01-02", "buy", 5)
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q)
	if s := orderStatus(t, d); s != "open" {
		t.Errorf("status = %q with only a stale price, want open", s)
	}
}

func TestQueuedMarketOrderFillsAtTheOpen(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "buy", 5)
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q)

	if s := orderStatus(t, d); s != "filled" {
		t.Fatalf("status = %q, want filled", s)
	}
	p, _ := q.GetParticipant(t.Context(), 1)
	if p.CashBalance != 100000-5*1050 {
		t.Errorf("cash = %d, want %d", p.CashBalance, 100000-5*1050)
	}
	h, _ := q.GetHolding(t.Context(), db.GetHoldingParams{ParticipantID: 1, StockID: 1})
	if h.Quantity != 5 || h.AvgCost != 1050 {
		t.Errorf("holding = %d at %d, want 5 at 1050", h.Quantity, h.AvgCost)
	}
	if got := notificationTitle(d); got != "Queued Order Filled" {
		t.Errorf("notification = %q, want Queued Order Filled", got)
	}
}

func TestQueuedBuyCancelledWithoutCash(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "buy", 1000) // $10,500 against $1,000 cash
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q)

	if s := orderStatus(t, d); s != "cancelled" {
		t.Errorf("status = %q, want cancelled", s)
	}
	if got := notificationTitle(d); got != "Queued Order Cancelled" {
		t.Errorf("notification = %q, want Queued Order Cancelled", got)
	}
}

// TestQueuedSellCanGoShort: the game allows shorting, so a queued sell with no
// holding opens a short position, as a market sell would while open.
func TestQueuedSellCanGoShort(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "sell", 3)
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q)

	if s := orderStatus(t, d); s != "filled" {
		t.Fatalf("status = %q, want filled", s)
	}
	h, _ := q.GetHolding(t.Context(), db.GetHoldingParams{ParticipantID: 1, StockID: 1})
	if h.Quantity != -3 || h.AvgCost != 1050 {
		t.Errorf("holding = %d at %d, want -3 at 1050", h.Quantity, h.AvgCost)
	}
}
