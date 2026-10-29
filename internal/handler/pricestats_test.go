package handler

import (
	"bytes"
	"strings"
	"testing"

	"github.com/exploded/game/internal/db"
)

func renderPriceStats(t *testing.T, ps priceStats) string {
	t.Helper()
	pages := loadTestTemplates(t)
	var buf bytes.Buffer
	err := pages["stocks/detail"].ExecuteTemplate(&buf, "price-stats", PageData{
		Extra: map[string]any{"PriceStats": ps},
	})
	if err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestPriceStatsPollsOnlyWhileOpen(t *testing.T) {
	ps := priceStats{
		GameID: 3, StockID: 7,
		Latest:     db.StockPrice{ID: 1, Date: "2026-10-29", Open: 34692, High: 34718, Low: 34352, Close: 34383},
		ChangeText: "-$3.27 (-0.94%)",
		Open:       true,
		AsOf:       "As of 10:42 am ET",
	}
	open := renderPriceStats(t, ps)
	for _, want := range []string{
		`hx-get="/games/3/stocks/7/price"`, `hx-trigger="every 60s"`,
		"$343.83", "-$3.27 (-0.94%)", "text-red", "Market open", "As of 10:42 am ET",
		`data-date="2026-10-29"`, `data-c="34383"`,
	} {
		if !strings.Contains(open, want) {
			t.Errorf("open header missing %q:\n%s", want, open)
		}
	}

	ps.Open, ps.AsOf = false, "Close, 29 Oct"
	closed := renderPriceStats(t, ps)
	if strings.Contains(closed, "hx-trigger") {
		t.Errorf("closed header still polls:\n%s", closed)
	}
	if !strings.Contains(closed, "Market closed") || !strings.Contains(closed, "Close, 29 Oct") {
		t.Errorf("closed header missing its status:\n%s", closed)
	}
}

func TestPriceStatsEmptyWithoutPrices(t *testing.T) {
	if got := strings.TrimSpace(renderPriceStats(t, priceStats{})); got != "" {
		t.Errorf("want nothing without a price row, got:\n%s", got)
	}
}
