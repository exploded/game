package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
)

// dailyPrices makes a close for every calendar day from start, n days long,
// closing at 100 cents plus the day's number.
func dailyPrices(t *testing.T, start string, n int) []db.StockPrice {
	t.Helper()
	d, err := time.Parse("2006-01-02", start)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]db.StockPrice, n)
	for i := range out {
		out[i] = db.StockPrice{Date: d.AddDate(0, 0, i).Format("2006-01-02"), Close: int64(100 + i)}
	}
	return out
}

func TestLineSeriesRanges(t *testing.T) {
	// 2020-01-01 to 2026-11-05: long enough for every range.
	prices := dailyPrices(t, "2020-01-01", 2501)
	last := prices[len(prices)-1]
	got := lineSeries(prices)
	for _, tc := range []struct {
		key, from string
	}{
		{"1", "2026-10-05"}, {"6", "2026-05-05"}, {"12", "2025-11-05"}, {"60", "2021-11-05"}, {"0", "2020-01-01"},
	} {
		r, ok := got[tc.key]
		if !ok {
			t.Errorf("range %s missing", tc.key)
			continue
		}
		if r.D[0] != tc.from {
			t.Errorf("range %s starts %s, want %s", tc.key, r.D[0], tc.from)
		}
		if r.D[len(r.D)-1] != last.Date || r.C[len(r.C)-1] != last.Close {
			t.Errorf("range %s ends %s %d, want the last price", tc.key, r.D[len(r.D)-1], r.C[len(r.C)-1])
		}
		if len(r.D) > lineMaxPoints || len(r.D) != len(r.C) {
			t.Errorf("range %s has %d dates and %d closes, want the same and at most %d", tc.key, len(r.D), len(r.C), lineMaxPoints)
		}
	}
	// A month of calendar days stays whole.
	if n := len(got["1"].D); n != 32 {
		t.Errorf("1M has %d points, want all 32", n)
	}
}

func TestLineSeriesShortHistory(t *testing.T) {
	got := lineSeries(dailyPrices(t, "2026-09-01", 66)) // to 5 November
	for _, key := range []string{"6", "12", "60"} {
		if _, ok := got[key]; ok {
			t.Errorf("range %s kept, but the history is shorter", key)
		}
	}
	if _, ok := got["1"]; !ok {
		t.Error("1M missing")
	}
	if got["0"].D[0] != "2026-09-01" {
		t.Errorf("All starts %s, want the first price", got["0"].D[0])
	}

	one := lineSeries(dailyPrices(t, "2026-11-05", 1))
	if len(one) != 1 || len(one["0"].C) != 1 {
		t.Errorf("one price: got %+v, want All alone with one point", one)
	}
	if lineSeries(nil) != nil {
		t.Error("no prices should give nil")
	}
}

func TestThinLineKeepsEnds(t *testing.T) {
	for _, n := range []int{1, 2, 259, 260, 261, 517, 518, 5000} {
		r := thinLine(dailyPrices(t, "2000-01-03", n))
		if len(r.C) > lineMaxPoints {
			t.Errorf("n=%d: %d points, want at most %d", n, len(r.C), lineMaxPoints)
		}
		if r.C[0] != 100 || r.C[len(r.C)-1] != int64(100+n-1) {
			t.Errorf("n=%d: ends %d..%d, want 100..%d", n, r.C[0], r.C[len(r.C)-1], 100+n-1)
		}
	}
}

func TestStockBars(t *testing.T) {
	srv, _ := gameServer(t)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/games/1/stocks/1/bars", nil))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
		t.Errorf("Cache-Control %q, want it cached briefly", cc)
	}
	var bars []candleBar
	if err := json.Unmarshal(w.Body.Bytes(), &bars); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, w.Body.String())
	}
	if len(bars) == 0 || bars[len(bars)-1].C == 0 || bars[len(bars)-1].T == "" {
		t.Errorf("got %+v, want the stock's bars", bars)
	}

	w = httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/games/1/stocks/999/bars", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown stock: status %d, want 404", w.Code)
	}
}

// TestStockPageChartData checks the stock page carries the line's data and
// the candles' URLs, but doesn't load the candle library itself: phones
// never need it.
func TestStockPageChartData(t *testing.T) {
	srv, _ := gameServer(t)
	body := getPage(t, srv, "/games/1/stocks/1", "")
	for _, want := range []string{
		`<script type="application/json" id="chart-line"`,
		`"0":{"d":["2026-10-28"],"c":[1050]}`,
		`data-bars="/games/1/stocks/1/bars"`,
		`data-lib="/static/js/lightweight-charts-4.2.3.standalone.production`,
		`/static/js/stockchart`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stock page missing %q", want)
		}
	}
	if strings.Contains(body, `<script src="/static/js/lightweight-charts`) {
		t.Error("stock page loads the candle library up front")
	}
}
