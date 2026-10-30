package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
)

func TestAdminPrices(t *testing.T) {
	d := openTestDB(t)
	for _, stmt := range []string{
		`INSERT INTO stocks (id, symbol, name, market, currency) VALUES
			(1, 'ACME', 'Acme Corp', 'sp500', 'USD'), (2, 'GONE', 'Gone Inc', 'sp500', 'USD'), (3, 'NEWCO', 'New Co', 'sp500', 'USD')`,
		`INSERT INTO stock_prices (stock_id, date, open, high, low, close) VALUES
			(1, '2026-10-29', 100, 100, 100, 100), (2, '2026-10-20', 100, 100, 100, 100)`,
		`INSERT INTO exchange_rates (date, rate_aud_usd) VALUES ('2026-10-29', 652300)`,
		`INSERT INTO price_fetch_log (market, mode, status, stocks_updated, symbols_ok, symbols_skipped, error_msg, duration_ms)
			VALUES ('sp500', 'daily', 'partial', 538, 538, 1, 'twelve data error 500: oops', 4200)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}
	h := New(db.New(d), d, loadTestTemplates(t), false, time.UTC)

	w := httptest.NewRecorder()
	h.AdminPrices(w, httptest.NewRequest("GET", "/admin/prices", nil))
	body := w.Body.String()
	if w.Code != 200 {
		t.Fatalf("status %d:\n%s", w.Code, body)
	}
	for _, want := range []string{
		"1 of 3", "29 Oct 2026", // ACME is up to date
		"Behind (2)", "NEWCO", "GONE", "20 Oct 2026", // furthest behind, and never priced
		"0.6523",
		"Daily", "partial", "4.2 s", "twelve data error 500: oops",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/admin/prices doesn't contain %q", want)
		}
	}
	// NEWCO has no prices at all, so it sorts before GONE.
	if strings.Index(body, "NEWCO") > strings.Index(body, "GONE") {
		t.Error("the stock with no prices isn't listed first")
	}
}

// TestAdminPricesWithNoPrices: before the first backfill nothing is up to
// date, even though every stock's newest date is equally "none".
func TestAdminPricesWithNoPrices(t *testing.T) {
	d := openTestDB(t)
	if _, err := d.Exec(`INSERT INTO stocks (symbol, name, market) VALUES ('ACME', 'Acme Corp', 'sp500')`); err != nil {
		t.Fatal(err)
	}
	h := New(db.New(d), d, loadTestTemplates(t), false, time.UTC)
	w := httptest.NewRecorder()
	h.AdminPrices(w, httptest.NewRequest("GET", "/admin/prices", nil))
	if body := w.Body.String(); !strings.Contains(body, "0 of 1") || !strings.Contains(body, "No prices yet") {
		t.Errorf("with no prices, the page doesn't say none are up to date:\n%s", body)
	}
}
