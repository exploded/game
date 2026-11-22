package market

import (
	"context"
	"slices"
	"testing"
	"time"
)

// TestProfilesDue: a profile with no statistics (a new listing whose
// /statistics isn't ready yet) is tried again the next day, not a week later.
func TestProfilesDue(t *testing.T) {
	d, q := monthlyDB(t)
	now := time.Date(2026, 11, 21, 23, 0, 0, 0, time.UTC)
	ago := func(age time.Duration) string { return now.Add(-age).Format(time.DateTime) }
	if _, err := d.Exec(`INSERT INTO stocks (id, symbol, name, market, currency) VALUES
		(1, 'NONE', 'No profile', 'sp500', 'USD'),
		(2, 'FRESH', 'Full, 1 day old', 'sp500', 'USD'),
		(3, 'STALE', 'Full, 7 days old', 'sp500', 'USD'),
		(4, 'NEWSTAT', 'No statistics, 2 hours old', 'sp500', 'USD'),
		(5, 'OLDSTAT', 'No statistics, 1 day old', 'sp500', 'USD')`); err != nil {
		t.Fatal(err)
	}
	for _, p := range []struct {
		id        int
		marketCap int64
		updated   string
	}{
		{2, 5e9, ago(24 * time.Hour)},
		{3, 5e9, ago(7 * 24 * time.Hour)},
		{4, 0, ago(2 * time.Hour)},
		{5, 0, ago(23 * time.Hour)},
	} {
		if _, err := d.Exec(`INSERT INTO stock_profiles (stock_id, market_cap, updated_at) VALUES (?, ?, ?)`,
			p.id, p.marketCap, p.updated); err != nil {
			t.Fatal(err)
		}
	}

	stocks, err := q.ListStocksNeedingProfile(context.Background(), ProfilesDue(now))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range stocks {
		got = append(got, s.Symbol)
	}
	if want := []string{"NONE", "OLDSTAT", "STALE"}; !slices.Equal(got, want) {
		t.Errorf("due: got %v, want %v", got, want)
	}
}
