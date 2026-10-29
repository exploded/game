package market

import (
	"testing"

	"github.com/exploded/game/internal/db"
)

func TestLimitStocks(t *testing.T) {
	stocks := []db.Stock{{Symbol: "AAPL"}, {Symbol: "MSFT"}, {Symbol: "BRK.B"}, {Symbol: "IBM"}}

	t.Setenv("TWELVEDATA_SYMBOLS", "")
	if got := LimitStocks(stocks); len(got) != 4 {
		t.Errorf("unset: got %d stocks, want all 4", len(got))
	}

	t.Setenv("TWELVEDATA_SYMBOLS", " msft, brk.b ,,IBM ")
	got := LimitStocks(stocks)
	if len(got) != 3 || got[0].Symbol != "MSFT" || got[1].Symbol != "BRK.B" || got[2].Symbol != "IBM" {
		t.Errorf("got %+v, want MSFT, BRK.B, IBM", got)
	}
}
