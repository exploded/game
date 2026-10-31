package handler

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/exploded/game/internal/db"
)

// TestWatchlistKeptAcrossGames: a stock watched in one month's game is still
// watched in the next, and can be taken off from there.
func TestWatchlistKeptAcrossGames(t *testing.T) {
	h, q := achievementsDB(t)
	ctx := context.Background()
	if err := q.InsertStock(ctx, db.InsertStockParams{Symbol: "AAPL", Name: "Apple", Market: "sp500", Currency: "USD"}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.rawDB.Exec(`
		INSERT INTO games (id, created_by, name, status, start_date, end_date) VALUES
			(2, 1, 'November 2026', 'active', '2026-11-01', '2026-11-30');
		INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES (2, 2, 1, 0, 0);`); err != nil {
		t.Fatal(err)
	}
	watching := func() bool {
		n, _ := q.IsWatching(ctx, db.IsWatchingParams{UserID: 1, StockID: 1})
		return n > 0
	}

	r := httptest.NewRequest("POST", "/", nil)
	r.SetPathValue("id", "1")
	r.SetPathValue("sid", "1")
	h.WatchlistAdd(httptest.NewRecorder(), asAlice(t, q, r))
	if !watching() {
		t.Fatal("AAPL not watched after adding it in game 1")
	}

	rows := []screenRow{{ListScreenerStocksRow: db.ListScreenerStocksRow{ID: 1}}}
	h.markRows(ctx, 2, 1, rows)
	if !rows[0].Watching {
		t.Error("game 2's stocks page doesn't star AAPL")
	}

	r = httptest.NewRequest("DELETE", "/", nil)
	r.SetPathValue("id", "2")
	r.SetPathValue("sid", "1")
	h.WatchlistRemove(httptest.NewRecorder(), asAlice(t, q, r))
	if watching() {
		t.Error("AAPL still watched after removing it in game 2")
	}
}
