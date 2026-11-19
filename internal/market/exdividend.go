package market

import (
	"context"

	"github.com/exploded/game/internal/db"
)

// ExDividendClose is a stored close less any dividends that went ex after the
// close's date. Dividends are paid at 10:00 UTC on the ex-date, but until the
// first quote of that session the newest close is the day before's, which
// still includes the dividend. Filling or valuing at it would let a player
// buy at that close, collect the dividend and sell at the same close (found
// in review, 19 November 2026). The result is never below 1 cent.
func ExDividendClose(ctx context.Context, q *db.Queries, stockID int64, p db.StockPrice) int64 {
	perShare, err := q.DividendsAfter(ctx, db.DividendsAfterParams{StockID: stockID, ExDate: p.Date})
	if err != nil || perShare <= 0 {
		return p.Close
	}
	// per_share is dollars × 1,000,000, so cents × 10,000.
	return max(p.Close-divRound(perShare, 10000), 1)
}
