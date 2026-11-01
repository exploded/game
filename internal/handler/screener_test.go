package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
)

func TestSparkPoints(t *testing.T) {
	for _, c := range []struct {
		name   string
		closes []int64
		want   string
	}{
		{"one close", []int64{100}, ""},
		{"rising", []int64{100, 150, 200}, "0.0,19.0 30.0,10.0 60.0,1.0"},
		{"falling", []int64{200, 100}, "0.0,1.0 60.0,19.0"},
		{"flat", []int64{100, 100, 100}, "0.0,10.0 30.0,10.0 60.0,10.0"},
	} {
		if got := sparkPoints(c.closes); got != c.want {
			t.Errorf("%s: sparkPoints(%v) = %q, want %q", c.name, c.closes, got, c.want)
		}
	}
}

func TestLogoURL(t *testing.T) {
	if got := logoURL("https://api.twelvedata.com/logo/microsoft.com"); got == "" {
		t.Error("dropped a Twelve Data logo")
	}
	if got := logoURL("https://static2.finnhub.io/file/publicdatany/finnhubimage/stock_logo/V.png"); got != "" {
		t.Errorf("kept a logo the CSP blocks: %q", got)
	}
}

func TestNewScreenRow(t *testing.T) {
	bars := []db.ListRecentClosesRow{
		{Date: "2026-09-01", Close: 900, Volume: 1},  // more than 30 days back: no spark
		{Date: "2026-10-01", Close: 1000, Volume: 1}, // the spark starts here
		{Date: "2026-10-29", Close: 1100, Volume: 7},
		{Date: "2026-10-30", Close: 1045, Volume: 10},
	}
	r := newScreenRow(db.ListScreenerStocksRow{Symbol: "ACME"}, bars)
	if r.Price != 1045 || r.Today.Amount != -55 || r.Today.Base != 1100 {
		t.Errorf("price %d, today %+v; want 1045, -55 on 1100", r.Price, r.Today)
	}
	if r.Turnover != 10450 {
		t.Errorf("turnover %d, want 10450 (10 shares at 1045)", r.Turnover)
	}
	if n := len(strings.Fields(r.Spark)); n != 3 || !r.SparkUp {
		t.Errorf("spark %q (up %v): want 3 points, up", r.Spark, r.SparkUp)
	}
	if r := newScreenRow(db.ListScreenerStocksRow{Symbol: "NONE"}, nil); r.Price != 0 || r.Spark != "" {
		t.Errorf("a stock with no bars got %+v", r)
	}
}

// screenStock makes a row with a price, a move against prev and a market
// cap, for applyFilter.
func screenStock(id int64, symbol, sector string, prev, price, volume, mktCap int64) screenRow {
	r := newScreenRow(db.ListScreenerStocksRow{
		ID: id, Symbol: symbol, Name: symbol + " Inc.", Market: "sp500", Sector: sector, MarketCap: mktCap,
	}, []db.ListRecentClosesRow{{Date: "2026-10-29", Close: prev}, {Date: "2026-10-30", Close: price, Volume: volume}})
	return r
}

func symbols(rows []screenRow) string {
	s := make([]string, len(rows))
	for i, r := range rows {
		s[i] = r.Symbol
	}
	return strings.Join(s, " ")
}

func TestApplyFilter(t *testing.T) {
	rows := []screenRow{
		screenStock(1, "AAPL", "Information Technology", 100, 102, 50, 3000), // +2%
		screenStock(2, "BHP", "Materials", 100, 95, 10, 150),                 // -5%
		screenStock(3, "MSFT", "Information Technology", 100, 101, 1000, 3500),
		screenStock(4, "XOM", "Energy", 100, 110, 1, 400), // +10%
		screenStock(5, "F", "Consumer Discretionary", 100, 100, 9000, 50),
		screenStock(6, "TSM", "Information Technology", 100, 99, 0, 1200),
		screenStock(7, "IBIT", "Financials", 100, 100, 0, 0),
		screenStock(8, "COIN", "Financials", 100, 100, 0, 90),
	}
	rows[2].IsPick, rows[2].PickOrder = 1, 1
	rows[3].IsPick, rows[3].PickOrder = 1, 2
	rows[0].IsPick, rows[0].PickOrder = 1, 3
	before := symbols(rows)

	for _, c := range []struct {
		f, sector, q string
		want         string
	}{
		{"", "", "", "MSFT XOM AAPL"}, // the default is Rob's Picks, in Rob's order
		{"picks", "Information Technology", "", "MSFT AAPL"},
		{"mag7", "", "", "MSFT AAPL"},
		{"tech", "", "", "MSFT AAPL TSM"},
		{"up", "", "", "XOM AAPL MSFT"},
		{"down", "", "", "BHP TSM"},
		{"big", "", "", "MSFT AAPL TSM XOM BHP COIN F IBIT"},
		{"active", "", "", "F MSFT AAPL BHP XOM"}, // dollar volume, not shares
		{"aus", "", "", "BHP"},
		{"intl", "", "", "TSM"},
		{"crypto", "", "", "COIN IBIT"},
		{"all", "Energy", "", "XOM"},
		{"nonsense", "", "", "MSFT XOM AAPL"},
		// A search ignores the chip and sector. An exact symbol comes first.
		{"down", "Energy", "f", "F MSFT"},
		{"up", "", "m", "MSFT XOM TSM"},
	} {
		got := applyFilter(rows, c.f, c.sector, "", c.q, nil)
		if s := symbols(got.Rows); s != c.want {
			t.Errorf("f=%q sector=%q q=%q: got %q, want %q", c.f, c.sector, c.q, s, c.want)
		}
	}
	if s := symbols(rows); s != before {
		t.Errorf("applyFilter reordered the cached rows: %q", s)
	}

	fav := applyFilter(rows, "popular", "", "", "", map[int64]int64{2: 3, 4: 5})
	if s := symbols(fav.Rows); s != "XOM BHP" || fav.Rows[0].Holders != 5 {
		t.Errorf("favourites: got %q (%+v)", s, fav.Rows)
	}
	if rows[3].Holders != 0 {
		t.Error("favourites set Holders on the cached rows")
	}
}

// finStock makes a row with financials, for the Financials menu. Ratios are
// × 100 and growth and margins in basis points, as stock_profiles stores
// them.
func finStock(id int64, symbol, sector string, today int64, s db.ListScreenerStocksRow) screenRow {
	s.ID, s.Symbol, s.Name, s.Market, s.Sector = id, symbol, symbol+" Inc.", "sp500", sector
	return newScreenRow(s, []db.ListRecentClosesRow{{Date: "2026-10-29", Close: 100}, {Date: "2026-10-30", Close: 100 + today}})
}

func TestFinancialsMenu(t *testing.T) {
	rows := []screenRow{
		// Healthy: profit, free cash flow, low debt, current ratio 2, growing.
		finStock(1, "GOOD", "Information Technology", 1, db.ListScreenerStocksRow{
			MarketCap: 1000_000, Revenue: 500, NetIncome: 100, ProfitMargin: 2500, FreeCashFlow: 50_000,
			TrailingPe: 1200, PegRatio: 80, QuarterlyRevGrowth: 2000, QuarterlyEarnGrowth: 1000,
			TotalCash: 300, TotalDebt: 100, DebtToEquity: 20, CurrentRatio: 200,
		}),
		// Twelve Data gives this loss-maker a P/E and earnings growth.
		finStock(2, "LOSS", "Industrials", 3, db.ListScreenerStocksRow{
			MarketCap: 500_000, Revenue: 500, NetIncome: -2_000_000_000, ProfitMargin: -800, FreeCashFlow: -10,
			TrailingPe: 900, PegRatio: 50, QuarterlyRevGrowth: -500, QuarterlyEarnGrowth: 4000,
			TotalCash: 10, TotalDebt: 900, DebtToEquity: 300, CurrentRatio: 80,
		}),
		// Debts larger than its assets, so a negative debt-to-equity.
		finStock(3, "NEGQ", "Consumer Discretionary", 2, db.ListScreenerStocksRow{
			MarketCap: 2000_000, Revenue: 500, NetIncome: 100, ProfitMargin: 3000, FreeCashFlow: 20_000,
			TrailingPe: 1400, QuarterlyRevGrowth: 1500, QuarterlyEarnGrowth: 100,
			TotalCash: 10, TotalDebt: 900, DebtToEquity: -25, CurrentRatio: 90,
		}),
		// No debt at all.
		finStock(4, "NODT", "Information Technology", 0, db.ListScreenerStocksRow{
			MarketCap: 100_000, Revenue: 500, NetIncome: 50, ProfitMargin: 1000, FreeCashFlow: 5_000,
			TrailingPe: 3000, QuarterlyRevGrowth: 100, TotalCash: 400, CurrentRatio: 300,
		}),
		// A bank: plenty of cash against debt, but no balance sheet tests.
		finStock(5, "BANK", "Financials", 0, db.ListScreenerStocksRow{
			MarketCap: 3000_000, Revenue: 500, NetIncome: 100, ProfitMargin: 2100, FreeCashFlow: 1,
			TrailingPe: 1000, TotalCash: 5000, TotalDebt: 100, DebtToEquity: 2,
		}),
		// A fund: no financials, so zeros everywhere.
		finStock(6, "FUND", "Financials", 0, db.ListScreenerStocksRow{MarketCap: 0}),
		finStock(7, "ZERO", "Information Technology", 0, db.ListScreenerStocksRow{MarketCap: 900_000}),
	}

	for _, c := range []struct {
		f, fin, want string
	}{
		{"all", "", "GOOD LOSS NEGQ NODT BANK FUND ZERO"}, // All keeps the rows' order
		{"all", "nonsense", "GOOD LOSS NEGQ NODT BANK FUND ZERO"},
		{"all", "profit", "GOOD NEGQ NODT BANK"},
		{"all", "loss", "LOSS"},
		{"all", "fcf", "GOOD NODT NEGQ BANK"}, // by yield: 5%, 5% (the chip's order breaks the tie), 1%, 0%
		{"all", "margin", "NEGQ GOOD BANK"},   // highest first
		{"all", "pe", "BANK GOOD NEGQ"},       // lowest first; LOSS has a P/E but no profit
		{"all", "peg", "GOOD"},                // LOSS's PEG doesn't count
		{"all", "revgrowth", "GOOD NEGQ"},     // 15% counts
		{"all", "earngrowth", "GOOD NEGQ"},    // LOSS's growth doesn't count
		{"all", "lowdebt", "NODT GOOD"},       // not NEGQ, BANK or ZERO
		{"all", "netcash", "NODT GOOD"},       // not BANK
		{"all", "score", "GOOD NODT"},         // both 5, by market cap; NEGQ has 3
		{"tech", "profit", "GOOD NODT"},       // a chip's own order, biggest first
		{"up", "pe", "NEGQ GOOD"},             // Gainers keep their order
		{"big", "pe", "BANK NEGQ GOOD"},       // so does Biggest
		{"down", "loss", ""},
	} {
		got := applyFilter(rows, c.f, "", c.fin, "", nil)
		if s := symbols(got.Rows); s != c.want {
			t.Errorf("f=%q fin=%q: got %q, want %q", c.f, c.fin, s, c.want)
		}
	}

	pe := applyFilter(rows, "all", "Information Technology", "pe", "", nil)
	if pe.Caption != "Every stock in Information Technology with a P/E under 15, lowest first" || pe.Explain == "" {
		t.Errorf("caption %q, explain %q", pe.Caption, pe.Explain)
	}
	if len(pe.Rows) != 1 || pe.Rows[0].Figure != "P/E 12.0" {
		t.Errorf("P/E rows: %+v", pe.Rows)
	}
	for _, c := range []struct{ f, fin, want string }{
		{"all", "profit", "Every stock making a profit, A–Z"},
		{"tech", "profit", "Tech stocks making a profit, biggest first"},
		{"up", "pe", "Today's biggest gainers with a P/E under 15"},
	} {
		if got := applyFilter(rows, c.f, "", c.fin, "", nil).Caption; got != c.want {
			t.Errorf("f=%q fin=%q: caption %q, want %q", c.f, c.fin, got, c.want)
		}
	}
	if rows[0].Figure != "" {
		t.Error("applyFilter set Figure on the cached rows")
	}
	for fin, want := range map[string]string{
		"loss":    "Loss $2.00B",
		"fcf":     "Free cash flow yield 5.0%",
		"margin":  "Profit margin 30.0%",
		"netcash": "Net cash $400",
	} {
		if got := applyFilter(rows, "all", "", fin, "", nil).Rows[0].Figure; got != want {
			t.Errorf("%s: figure %q, want %q", fin, got, want)
		}
	}
	if got := applyFilter(rows, "all", "", "lowdebt", "", nil).Rows; got[0].Figure != "No debt" || got[1].Figure != "Debt-to-equity 0.20" {
		t.Errorf("lowdebt figures: %q, %q", got[0].Figure, got[1].Figure)
	}
	if got := applyFilter(rows, "all", "", "", "goo", nil); got.Explain != "" || symbols(got.Rows) != "GOOD" {
		t.Errorf("a search used the menu: %+v", got)
	}
}

func TestHealthScore(t *testing.T) {
	for _, c := range []struct {
		name  string
		row   db.ListScreenerStocksRow
		score int
		has   bool
	}{
		{"every point", db.ListScreenerStocksRow{Revenue: 1, NetIncome: 1, FreeCashFlow: 1, TotalDebt: 1, DebtToEquity: 99, CurrentRatio: 101, QuarterlyRevGrowth: 1}, 5, true},
		{"no points", db.ListScreenerStocksRow{Revenue: 1, NetIncome: -1, FreeCashFlow: -1, TotalDebt: 1, DebtToEquity: 100, CurrentRatio: 100, QuarterlyRevGrowth: -1}, 0, true},
		{"no debt counts", db.ListScreenerStocksRow{Revenue: 1}, 1, true},
		{"negative equity doesn't", db.ListScreenerStocksRow{Revenue: 1, TotalDebt: 1, DebtToEquity: -5}, 0, true},
		{"missing debt-to-equity doesn't", db.ListScreenerStocksRow{Revenue: 1, TotalDebt: 1}, 0, true},
		{"no financials", db.ListScreenerStocksRow{NetIncome: 1}, 0, false},
	} {
		r := finStock(1, "X", "Energy", 0, c.row)
		if r.Health != c.score || r.HasHealth != c.has {
			t.Errorf("%s: score %d (%v), want %d (%v)", c.name, r.Health, r.HasHealth, c.score, c.has)
		}
	}
	bank := finStock(1, "B", "Financials", 0, db.ListScreenerStocksRow{Revenue: 1, NetIncome: 1, FreeCashFlow: 1})
	if bank.HasHealth {
		t.Error("a bank got a health score")
	}
}

func getStocks(srv http.Handler, query string, headers map[string]string) string {
	req := httptest.NewRequest("GET", "/games/1/stocks"+query, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	return w.Body.String()
}

func TestStocksPage(t *testing.T) {
	srv, q := gameServer(t)
	if err := q.AddStockPick(context.Background(), db.AddStockPickParams{StockID: 1, Note: "Rockets for everyone"}); err != nil {
		t.Fatal(err)
	}
	// Before the first request, which caches the rows for a minute.
	if err := q.UpsertStockStatistics(context.Background(), db.UpsertStockStatisticsParams{
		StockID: 1, Revenue: 500, NetIncome: 100, TrailingPe: 1240, CurrentRatio: 200,
	}); err != nil {
		t.Fatal(err)
	}

	page := getStocks(srv, "", nil)
	for _, want := range []string{"<html", "Rob&#39;s Picks", `value="picks" class="sr-only" checked`, "ACME", "Rockets for everyone", "US$10.50"} {
		if !strings.Contains(page, want) {
			t.Errorf("stocks page doesn't contain %q", want)
		}
	}

	list := getStocks(srv, "?f=all", map[string]string{"HX-Request": "true"})
	if strings.Contains(list, "<html") || !strings.Contains(list, "Every stock, A–Z") || !strings.Contains(list, "ACME") {
		t.Errorf("htmx request didn't get just the list:\n%s", list)
	}
	if strings.Contains(list, "Rockets for everyone") {
		t.Error("the note shows outside Rob's Picks")
	}

	back := getStocks(srv, "?f=all", map[string]string{"HX-Request": "true", "HX-History-Restore-Request": "true"})
	if !strings.Contains(back, "<html") {
		t.Error("Back (a history restore) didn't get the whole page")
	}

	pe := getStocks(srv, "?f=all&fin=pe", nil)
	for _, want := range []string{`<option value="pe" selected>P/E under 15</option>`, "with a P/E under 15", "P/E is the share price", "P/E 12.4", "Health 3/5"} {
		if !strings.Contains(pe, want) {
			t.Errorf("?fin=pe doesn't contain %q", want)
		}
	}
	if odd := getStocks(srv, "?f=all&fin=nonsense", map[string]string{"HX-Request": "true"}); !strings.Contains(odd, "ACME") || strings.Contains(odd, "screener-explain") {
		t.Errorf("an unknown fin wasn't ignored:\n%s", odd)
	}

	found := getStocks(srv, "?f=down&q=acm", map[string]string{"HX-Request": "true"})
	if !strings.Contains(found, "Results for") || !strings.Contains(found, "ACME") {
		t.Errorf("search didn't find ACME:\n%s", found)
	}
}

// TestSeedRobPicks: the first picks go in once, in Rob's order, after the
// stocks are seeded, and a removed pick stays removed.
func TestSeedRobPicks(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.Exec(`INSERT INTO stocks (symbol, name, market) VALUES
		('GOOG', 'g', 'sp500'), ('NVDA', 'n', 'sp500'), ('META', 'm', 'sp500'),
		('ASML', 'a', 'sp500'), ('MSFT', 'ms', 'sp500'), ('V', 'v', 'sp500'), ('AAPL', 'ap', 'sp500')`); err != nil {
		t.Fatal(err)
	}
	if err := RunSeedMigrations(d); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	picks, err := q.ListStockPicks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, p := range picks {
		got = append(got, p.Symbol)
	}
	if s := strings.Join(got, " "); s != "V MSFT ASML META NVDA GOOG" {
		t.Errorf("picks %q", s)
	}

	if err := q.DeleteStockPick(context.Background(), picks[0].StockID); err != nil {
		t.Fatal(err)
	}
	if err := RunSeedMigrations(d); err != nil {
		t.Fatal(err)
	}
	if picks, _ := q.ListStockPicks(context.Background()); len(picks) != 5 {
		t.Errorf("the seed ran again: %d picks", len(picks))
	}
}

func TestAdminPicks(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.Exec(`INSERT INTO stocks (id, symbol, name, market) VALUES (1, 'NVDA', 'NVIDIA', 'sp500')`); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	h := New(q, d, loadTestTemplates(t), false, time.UTC)
	post := func(fn http.HandlerFunc, sid string, form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/admin/picks", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetPathValue("sid", sid)
		w := httptest.NewRecorder()
		fn(w, req)
		return w
	}
	pick := func() (db.StockPick, error) { return q.GetStockPick(context.Background(), 1) }

	post(h.AdminPickAdd, "", url.Values{"symbol": {"msft"}})
	if picks, _ := q.ListStockPicks(context.Background()); len(picks) != 0 {
		t.Error("added a stock that doesn't exist")
	}
	post(h.AdminPickAdd, "", url.Values{"symbol": {" nvda "}, "note": {"AI  chips"}})
	if p, err := pick(); err != nil || p.Note != "AI chips" {
		t.Fatalf("after adding: %+v, %v", p, err)
	}
	post(h.AdminPickUpdate, "1", url.Values{"note": {strings.Repeat("x", maxPickNote+1)}, "sort_order": {"4"}})
	if p, _ := pick(); p.Note != "AI chips" {
		t.Error("saved a note that's too long")
	}
	post(h.AdminPickUpdate, "1", url.Values{"note": {"Picks and shovels"}, "sort_order": {"4"}})
	if p, _ := pick(); p.Note != "Picks and shovels" || p.SortOrder != 4 {
		t.Errorf("after saving: %+v", p)
	}
	post(h.AdminPickDelete, "1", url.Values{})
	if _, err := pick(); err == nil {
		t.Error("the pick is still there")
	}
}
