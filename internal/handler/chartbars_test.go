package handler

import (
	"testing"

	"github.com/exploded/game/internal/db"
)

func bar(date string, o, h, l, c, v int64) db.StockPrice {
	return db.StockPrice{Date: date, Open: o, High: h, Low: l, Close: c, Volume: v}
}

func TestChartBars(t *testing.T) {
	prices := []db.StockPrice{
		// ISO week 2024-W01 runs Mon 1 Jan to Sun 7 Jan 2024.
		bar("2024-01-02", 100, 110, 95, 105, 10),
		bar("2024-01-03", 105, 120, 100, 115, 20),
		bar("2024-01-05", 115, 118, 90, 92, 30),
		// 2024-W02.
		bar("2024-01-08", 92, 93, 91, 93, 5),
		// Cutoff: these stay daily.
		bar("2024-01-10", 93, 99, 92, 98, 7),
		bar("2024-01-11", 98, 101, 97, 100, 8),
	}
	got := chartBars(prices, "2024-01-10")
	want := []db.StockPrice{
		bar("2024-01-02", 100, 120, 90, 92, 60),
		bar("2024-01-08", 92, 93, 91, 93, 5),
		bar("2024-01-10", 93, 99, 92, 98, 7),
		bar("2024-01-11", 98, 101, 97, 100, 8),
	}
	if len(got) != len(want) {
		t.Fatalf("got %d bars, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bar %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// 30 Dec 2024 is in ISO week 2025-W01, so it groups with 2 Jan 2025, not with
// the rest of December.
func TestChartBarsYearBoundary(t *testing.T) {
	prices := []db.StockPrice{
		bar("2024-12-27", 10, 10, 10, 10, 1),
		bar("2024-12-30", 20, 20, 20, 20, 1),
		bar("2025-01-02", 30, 30, 30, 30, 1),
	}
	got := chartBars(prices, "2099-01-01")
	if len(got) != 2 || got[1].Date != "2024-12-30" || got[1].Close != 30 {
		t.Errorf("got %+v, want 2024-12-27 alone, then 30 Dec and 2 Jan as one week", got)
	}
}

func TestChartBarsAllRecent(t *testing.T) {
	prices := []db.StockPrice{bar("2026-01-05", 1, 1, 1, 1, 1), bar("2026-01-06", 2, 2, 2, 2, 2)}
	if got := chartBars(prices, "2024-01-01"); len(got) != 2 {
		t.Errorf("recent bars should pass through unchanged, got %+v", got)
	}
}
