package market

import (
	"testing"

	"github.com/exploded/game/internal/db"
)

// TestRevalueWaitsForARate: 10 ACME at US$10.50 in an AUD game. With no
// AUD/USD rate stored the portfolio keeps its value instead of counting
// US$105 as A$105.
func TestRevalueWaitsForARate(t *testing.T) {
	q, d := ordersDB(t, "2026-10-28", "buy", 5)
	if _, err := d.Exec(`UPDATE games SET base_currency = 'AUD'`); err != nil {
		t.Fatal(err)
	}
	if err := q.UpsertHolding(t.Context(), db.UpsertHoldingParams{
		ParticipantID: 1, StockID: 1, Quantity: 10, AvgCost: 1000, CostBasis: 10000, CurrentValue: 10000,
	}); err != nil {
		t.Fatal(err)
	}
	value := func() int64 {
		t.Helper()
		p, err := q.GetParticipant(t.Context(), 1)
		if err != nil {
			t.Fatal(err)
		}
		return p.PortfolioValue
	}

	if err := RevaluePortfolios(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	if v := value(); v != 100000 {
		t.Errorf("value = %d with no rate, want it left at 100000", v)
	}

	// At 0.70, US$105.00 is A$150.00.
	if _, err := d.Exec(`INSERT INTO exchange_rates (date, rate_aud_usd) VALUES ('2026-10-28', 700000)`); err != nil {
		t.Fatal(err)
	}
	if err := RevaluePortfolios(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	if v := value(); v != 100000+15000 {
		t.Errorf("value = %d, want %d", v, 100000+15000)
	}
}
