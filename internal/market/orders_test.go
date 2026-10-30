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
		`INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x', 'Alice', 't1')`,
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
	if h.Quantity != 5 || h.AvgCost != 1050 || h.CostBasis != 5250 {
		t.Errorf("holding = %d at %d costing %d, want 5 at 1050 costing 5250", h.Quantity, h.AvgCost, h.CostBasis)
	}
	if got := notificationTitle(d); got != "Queued Order Filled" {
		t.Errorf("notification = %q, want Queued Order Filled", got)
	}
}

// limitOrder adds a limit order of 2 shares to ordersDB's player and returns
// its id.
func limitOrder(t *testing.T, q *db.Queries, side string, limit int64) int64 {
	t.Helper()
	o, err := q.CreateLimitOrder(t.Context(), db.CreateLimitOrderParams{
		ParticipantID: 1, StockID: 1, Type: side, Quantity: 2, LimitPrice: limit,
	})
	if err != nil {
		t.Fatal(err)
	}
	return o.ID
}

func statusOf(t *testing.T, d *sql.DB, id int64) string {
	t.Helper()
	var status string
	if err := d.QueryRow("SELECT status FROM limit_orders WHERE id = ?", id).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}

// TestLimitOrderWaitsWhileClosed: a limit buy above the last close mustn't
// fill at that close while the market is closed, but fills once it opens.
func TestLimitOrderWaitsWhileClosed(t *testing.T) {
	q, d := ordersDB(t, sessionToday(t), "buy", 5)
	id := limitOrder(t, q, "buy", 1100)
	setMarketOpen(t, false)
	ProcessLimitOrders(t.Context(), q)
	if s := statusOf(t, d, id); s != "open" {
		t.Fatalf("status = %q while the market is closed, want open", s)
	}

	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q)
	if s := statusOf(t, d, id); s != "filled" {
		t.Errorf("status = %q once the market opened, want filled", s)
	}
}

// TestLimitOrderWaitsForTodaysPrice: the market has opened, but the newest
// price is still yesterday's close.
func TestLimitOrderWaitsForTodaysPrice(t *testing.T) {
	sessionToday(t)
	q, d := ordersDB(t, "2020-01-02", "buy", 5)
	id := limitOrder(t, q, "buy", 1100)
	setMarketOpen(t, true)
	ProcessLimitOrders(t.Context(), q)
	if s := statusOf(t, d, id); s != "open" {
		t.Errorf("status = %q with only a stale price, want open", s)
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
	if h.Quantity != -3 || h.AvgCost != 1050 || h.CostBasis != -3150 {
		t.Errorf("holding = %d at %d costing %d, want -3 at 1050 costing -3150", h.Quantity, h.AvgCost, h.CostBasis)
	}
}

// TestOrdersCancelledWhenGameEnds: ordersDB's game ends on 31 December. Its
// queued order stays open that day and is cancelled, without a notification,
// once the game is over.
func TestOrdersCancelledWhenGameEnds(t *testing.T) {
	q, d := ordersDB(t, "2026-12-31", "buy", 5)
	if err := q.CancelOrdersInExpiredGames(t.Context(), "2026-12-31"); err != nil {
		t.Fatal(err)
	}
	if s := orderStatus(t, d); s != "open" {
		t.Errorf("status = %q on the game's last day, want open", s)
	}
	if err := q.CancelOrdersInExpiredGames(t.Context(), "2027-01-01"); err != nil {
		t.Fatal(err)
	}
	if s := orderStatus(t, d); s != "cancelled" {
		t.Errorf("status = %q after the game ended, want cancelled", s)
	}
	if title := notificationTitle(d); title != "" {
		t.Errorf("notification %q, want none", title)
	}
}
