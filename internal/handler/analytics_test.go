package handler

import (
	"testing"

	"github.com/exploded/game/internal/db"
)

func TestRealisedPnL(t *testing.T) {
	trade := func(stock int64, sym, side string, qty, total int64) db.ListAllTransactionsRow {
		return db.ListAllTransactionsRow{StockID: stock, Symbol: sym, Type: side, Quantity: qty, ConvertedTotal: total}
	}
	txns := []db.ListAllTransactionsRow{
		// AMD: an open short makes nothing yet, however much the sale raised.
		trade(1, "AMD", "sell", 300, 8001678),
		// AAPL: bought 10 at $100, sold 4 at $120: $80 made on the 4.
		trade(2, "AAPL", "buy", 10, 100000),
		trade(2, "AAPL", "sell", 4, 48000),
		// MSFT: shorted 5 at $200, bought back 5 at $150: $250 made.
		trade(3, "MSFT", "sell", 5, 100000),
		trade(3, "MSFT", "buy", 5, 75000),
		// NVDA: bought 2 at $100, sold 5 at $90 (flips to short 3), bought 3
		// back at $80: -$20 on the long, then +$30 on the short.
		trade(4, "NVDA", "buy", 2, 20000),
		trade(4, "NVDA", "sell", 5, 45000),
		trade(4, "NVDA", "buy", 3, 24000),
	}

	got := realisedPnL(txns)
	want := []StockPnL{{"AAPL", 8000}, {"MSFT", 25000}, {"NVDA", 1000}}
	if len(got) != len(want) {
		t.Fatalf("realisedPnL = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("realisedPnL[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}
