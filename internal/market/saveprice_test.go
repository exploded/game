package market

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
)

// TestSaveRowsFailedWriteNotSaved: a bar the database refuses (here a stock
// that doesn't exist, which breaks the foreign key) leaves its stock
// counted as not saved, so the daily fetch is tried again, while the other
// stocks in the batch are saved.
func TestSaveRowsFailedWriteNotSaved(t *testing.T) {
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(game.Schema); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'ACME', 'Acme', 'sp500', 'USD')`); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	f := &Fetcher{q: q, rawDB: d}
	bar := map[string]any{"datetime": "2026-11-18", "open": "10", "high": "11", "low": "9", "close": "10.5", "volume": "100"}
	saved, err := f.saveRows(context.Background(), q, []priceRow{{stockID: 1, v: bar}, {stockID: 99, v: bar}})
	if err != nil {
		t.Fatal(err)
	}
	if saved[1] != 1 {
		t.Errorf("stock 1 saved %d rows, want 1", saved[1])
	}
	if n, ok := saved[99]; ok {
		t.Errorf("stock 99's refused write counted as %d saved", n)
	}
	var rows int
	d.QueryRow(`SELECT COUNT(*) FROM stock_prices`).Scan(&rows)
	if rows != 1 {
		t.Errorf("%d price rows, want 1", rows)
	}
}
