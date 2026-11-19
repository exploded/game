package handler

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// stocksTest is a handler over a database with the seed stocks, with
// Twelve Data's lookup and the background fetch replaced.
func stocksTest(t *testing.T) (*sql.DB, *Handler, *[]string) {
	t.Helper()
	d := openTestDB(t)
	q := db.New(d)
	market.SeedStocks(context.Background(), q)
	if err := market.LoadListings(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	fetched := &[]string{}
	oldLookup, oldFetch := stockLookup, fetchNewStock
	stockLookup = func(_ context.Context, _ *Handler, symbol string) (market.FoundStock, error) {
		return market.FoundStock{Symbol: symbol, Name: "Anthropic, PBC", Exchange: "NASDAQ", Type: "Common Stock"}, nil
	}
	fetchNewStock = func(_ *Handler, s db.Stock) { *fetched = append(*fetched, s.Symbol) }
	t.Cleanup(func() { stockLookup, fetchNewStock = oldLookup, oldFetch })
	return d, New(q, d, loadTestTemplates(t), false, time.UTC), fetched
}

// formRequest is a POST of form, with the path's {sid} set to sid.
func formRequest(path string, form url.Values, sid string) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if sid != "" {
		r.SetPathValue("sid", sid)
	}
	return r
}

// TestAdminStockAdd: looking up a ticker fills the add form; adding it
// stores it on its list, fetches its prices and labels it at once; adding
// it again is refused.
func TestAdminStockAdd(t *testing.T) {
	d, h, fetched := stocksTest(t)

	w := httptest.NewRecorder()
	h.AdminStocks(w, httptest.NewRequest("GET", "/admin/stocks?lookup=ant", nil))
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("status %d:\n%s", w.Code, body)
	}
	for _, want := range []string{`name="symbol" value="ANT"`, `value="Anthropic, PBC"`, "Pick one", "Add ANT"} {
		if !strings.Contains(body, want) {
			t.Errorf("lookup page doesn't contain %q", want)
		}
	}

	add := url.Values{"symbol": {"ANT"}, "name": {"Anthropic"}, "sector": {"Information Technology"}, "listing": {"us"}}
	w = httptest.NewRecorder()
	h.AdminStockAdd(w, formRequest("/admin/stocks", add, ""))
	if w.Code != 303 {
		t.Fatalf("add: status %d", w.Code)
	}
	var listing, sector string
	if err := d.QueryRow(`SELECT listing, sector FROM stocks WHERE symbol = 'ANT' AND market = 'sp500'`).Scan(&listing, &sector); err != nil {
		t.Fatalf("ANT not added: %v", err)
	}
	if listing != "us" || sector != "Information Technology" {
		t.Errorf("ANT stored on %q in %q", listing, sector)
	}
	if len(*fetched) != 1 || (*fetched)[0] != "ANT" {
		t.Errorf("fetched %v, want ANT", *fetched)
	}
	if got := market.MarketLabel("ANT", "sp500"); got != "US" {
		t.Errorf("ANT labelled %q straight after adding, want US", got)
	}

	w = httptest.NewRecorder()
	h.AdminStockAdd(w, formRequest("/admin/stocks", add, ""))
	if !strings.Contains(w.Header().Get("Set-Cookie"), "already in the game") {
		t.Errorf("second add not refused: %v", w.Header())
	}

	bad := url.Values{"symbol": {"NEW"}, "name": {"New"}, "sector": {"Crypto"}, "listing": {"us"}}
	w = httptest.NewRecorder()
	h.AdminStockAdd(w, formRequest("/admin/stocks", bad, ""))
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM stocks WHERE symbol = 'NEW'`).Scan(&n)
	if n != 0 {
		t.Error("a stock with an unknown sector was added")
	}
}

// TestAdminStockRetire: a stock someone holds can't be retired; one nobody
// holds can, and can be brought back.
func TestAdminStockRetire(t *testing.T) {
	d, h, _ := stocksTest(t)
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x', 'A', 't1')`,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date)
			VALUES (1, 1, 'November', 'active', 'USD', '2026-11-01', '2026-11-30')`,
		`INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 1, 0, 0)`,
		`INSERT INTO holdings (participant_id, stock_id, quantity, avg_cost, cost_basis)
			SELECT 1, id, 5, 100, 500 FROM stocks WHERE symbol = 'AAPL'`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	id := func(sym string) string {
		var id string
		d.QueryRow(`SELECT id FROM stocks WHERE symbol = ?`, sym).Scan(&id)
		return id
	}
	active := func(sym string) (a int) {
		d.QueryRow(`SELECT is_active FROM stocks WHERE symbol = ?`, sym).Scan(&a)
		return
	}

	w := httptest.NewRecorder()
	h.AdminStockActive(w, formRequest("/admin/stocks/x/active", nil, id("AAPL")))
	if active("AAPL") != 1 || !strings.Contains(w.Header().Get("Set-Cookie"), "can't be retired") {
		t.Errorf("a held stock was retired, or no reason given: %v", w.Header())
	}

	h.AdminStockActive(httptest.NewRecorder(), formRequest("/admin/stocks/x/active", nil, id("MSFT")))
	if active("MSFT") != 0 {
		t.Error("MSFT, which nobody holds, wasn't retired")
	}
	w = httptest.NewRecorder()
	h.AdminStocks(w, httptest.NewRequest("GET", "/admin/stocks", nil))
	if !strings.Contains(w.Body.String(), "Retired") || !strings.Contains(w.Body.String(), "Bring back") {
		t.Error("the page doesn't list the retired stock")
	}
	h.AdminStockActive(httptest.NewRecorder(), formRequest("/admin/stocks/x/active", nil, id("MSFT")))
	if active("MSFT") != 1 {
		t.Error("MSFT wasn't brought back")
	}
}

// TestAdminStockListing: moving a stock to another list relabels it.
func TestAdminStockListing(t *testing.T) {
	d, h, _ := stocksTest(t)
	var sid string
	d.QueryRow(`SELECT id FROM stocks WHERE symbol = 'SPCX'`).Scan(&sid)
	t.Cleanup(func() {
		d.Exec(`UPDATE stocks SET listing = 'us' WHERE symbol = 'SPCX'`)
		market.LoadListings(context.Background(), db.New(d))
	})

	h.AdminStockListing(httptest.NewRecorder(), formRequest("/admin/stocks/x/listing", url.Values{"listing": {"sp500"}}, sid))
	if got := market.MarketLabel("SPCX", "sp500"); got != "SP500" {
		t.Errorf("SPCX moved to the S&P 500 is labelled %q", got)
	}
	w := httptest.NewRecorder()
	h.AdminStockListing(w, formRequest("/admin/stocks/x/listing", url.Values{"listing": {"moon"}}, sid))
	if w.Code != 400 {
		t.Errorf("unknown list: status %d, want 400", w.Code)
	}
}

// TestAdminStocksNewListings: new listings show biggest first with the
// S&P size badge, and Dismiss hides one.
func TestAdminStocksNewListings(t *testing.T) {
	d, h, _ := stocksTest(t)
	today := time.Now().Format(time.DateOnly)
	if _, err := d.Exec(`INSERT INTO us_listings (symbol, exchange, name, type, first_seen, market_cap) VALUES
		('ANT', 'NASDAQ', 'Anthropic, PBC', 'Common Stock', ?, 61500000000),
		('SMOL', 'NYSE', 'Small Co', 'Common Stock', ?, 0),
		('OLD', 'NYSE', 'Old Co', 'Common Stock', '2020-01-01', 0)`, today, today); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.AdminStocks(w, httptest.NewRequest("GET", "/admin/stocks", nil))
	body := w.Body.String()
	for _, want := range []string{"Anthropic, PBC", "US$61.5 billion", "S&amp;P size", "Small Co", "Not trading yet"} {
		if !strings.Contains(body, want) {
			t.Errorf("/admin/stocks doesn't contain %q", want)
		}
	}
	if strings.Contains(body, "Old Co") {
		t.Error("a listing from 2020 is still listed as new")
	}

	h.AdminListingDismiss(httptest.NewRecorder(), formRequest("/admin/stocks/listings/dismiss",
		url.Values{"symbol": {"SMOL"}, "exchange": {"NYSE"}}, ""))
	w = httptest.NewRecorder()
	h.AdminStocks(w, httptest.NewRequest("GET", "/admin/stocks", nil))
	if strings.Contains(w.Body.String(), "Small Co") {
		t.Error("a dismissed listing is still shown")
	}
}

// TestStockListingMigration: a database from before stocks.listing gets
// each stock's list from the old Go lists, and SeedStocks doesn't undo a
// change made after.
func TestStockListingMigration(t *testing.T) {
	old, err := os.ReadFile("testdata/schema-2026-10-30.sql")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "old.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		string(old),
		`INSERT INTO stocks (symbol, name, market, currency) VALUES
			('AAPL', 'Apple', 'sp500', 'USD'), ('BHP', 'BHP', 'sp500', 'USD'),
			('ASML', 'ASML', 'sp500', 'USD'), ('SPCX', 'SpaceX', 'sp500', 'USD')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatalf("old database: %v", err)
		}
	}
	raw.Close()

	d := openTestDBAt(t, path)
	q := db.New(d)
	market.SeedStocks(context.Background(), q)
	if err := RunSeedMigrations(d); err != nil {
		t.Fatal(err)
	}
	listing := func(sym string) (l string) {
		d.QueryRow(`SELECT listing FROM stocks WHERE symbol = ?`, sym).Scan(&l)
		return
	}
	for sym, want := range map[string]string{"AAPL": "sp500", "BHP": "aus", "ASML": "intl", "SPCX": "us", "MSFT": "sp500", "TEAM": "aus"} {
		if got := listing(sym); got != want {
			t.Errorf("%s on %q, want %q", sym, got, want)
		}
	}

	d.Exec(`UPDATE stocks SET listing = 'sp500' WHERE symbol = 'SPCX'`)
	market.SeedStocks(context.Background(), q)
	if got := listing("SPCX"); got != "sp500" {
		t.Errorf("SeedStocks put SPCX back on %q", got)
	}
}
