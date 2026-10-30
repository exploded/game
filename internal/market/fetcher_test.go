package market

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

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

// twelveDataStub answers every request with body, as Twelve Data would.
type twelveDataStub string

func (s twelveDataStub) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(s))),
		Request:    r,
	}, nil
}

// TestBackfillOnceForRecentListing: a stock listed a few weeks ago has
// fewer than 30 days of history to fetch. Once the backfill has saved it,
// it mustn't count as missing, or every restart fetches it again (and an
// admin's Fetch prices backfills it instead of fetching the day's prices).
func TestBackfillOnceForRecentListing(t *testing.T) {
	d, q := monthlyDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'NEWCO', 'New Co', 'sp500', 'USD')`); err != nil {
		t.Fatal(err)
	}

	var values []string
	day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for range 21 {
		values = append(values, fmt.Sprintf(`{"datetime":%q,"open":"10","high":"11","low":"9","close":"10.5","volume":"1000"}`, day.Format(time.DateOnly)))
		day = day.AddDate(0, 0, 1)
	}
	body := `{"meta":{"symbol":"NEWCO"},"status":"ok","values":[` + strings.Join(values, ",") + `]}`
	f := &Fetcher{q: q, rawDB: d, client: &http.Client{Transport: twelveDataStub(body)}}

	missing, err := q.ListStocksWithoutHistory(ctx, "sp500")
	if err != nil || len(missing) != 1 {
		t.Fatalf("before the backfill: %d missing (%v), want 1", len(missing), err)
	}
	if _, ok, _, err := f.fetchOneBatch(ctx, q, missing, 5000); err != nil || ok != 1 {
		t.Fatalf("backfill: %d ok, %v", ok, err)
	}
	if missing, _ := q.ListStocksWithoutHistory(ctx, "sp500"); len(missing) != 0 {
		t.Errorf("after backfilling 21 days, %s still counts as missing", missing[0].Symbol)
	}
}
