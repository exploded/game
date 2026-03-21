package market

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/exploded/game/internal/db"
)

// StartFakeLiveFeed runs a goroutine that updates today's prices with small random
// movements every interval, simulating a real-time market feed. Dev mode only.
func StartFakeLiveFeed(ctx context.Context, q *db.Queries, interval time.Duration) {
	slog.Info("fake live feed started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("fake live feed stopped")
			return
		case <-ticker.C:
			tickFakePrices(ctx, q)
		}
	}
}

func tickFakePrices(ctx context.Context, q *db.Queries) {
	today := time.Now().UTC().Format("2006-01-02")

	stocks, err := q.ListStocksByMarket(ctx, db.ListStocksByMarketParams{
		Market: "both", Column2: "both",
	})
	if err != nil {
		return
	}

	updated := 0
	for _, s := range stocks {
		// Get the current price for today (or latest if no today price).
		existing, err := q.GetLatestPrice(ctx, s.ID)
		if err != nil || existing.Close == 0 {
			continue
		}

		lastClose := existing.Close

		// Small random movement: -0.5% to +0.5% per tick.
		change := 1.0 + (rand.Float64()*0.01 - 0.005)
		newClose := int64(float64(lastClose) * change)
		if newClose < 100 {
			newClose = 100
		}

		// Generate realistic OHLC.
		spread := int64(float64(newClose) * 0.005)
		if spread < 1 {
			spread = 1
		}
		newOpen := lastClose // open at previous close
		newHigh := newClose + rand.Int64N(spread+1)
		newLow := newClose - rand.Int64N(spread+1)

		// Ensure high >= max(open, close) and low <= min(open, close).
		if newHigh < newOpen {
			newHigh = newOpen
		}
		if newHigh < newClose {
			newHigh = newClose
		}
		if newLow > newOpen {
			newLow = newOpen
		}
		if newLow > newClose {
			newLow = newClose
		}

		// If today's price already exists, update it. Otherwise create it.
		if existing.Date == today {
			// Update — keep original open, widen high/low, set new close.
			if newHigh > existing.High {
				newHigh = newHigh
			} else {
				newHigh = existing.High
			}
			if newLow < existing.Low {
				newLow = newLow
			} else {
				newLow = existing.Low
			}
			newOpen = existing.Open
		}

		vol := existing.Volume + rand.Int64N(50000)

		_ = q.UpsertPrice(ctx, db.UpsertPriceParams{
			StockID: s.ID,
			Date:    today,
			Open:    newOpen,
			High:    newHigh,
			Low:     newLow,
			Close:   newClose,
			Volume:  vol,
		})
		updated++
	}

	// Also tick the exchange rate slightly.
	rate, err := q.GetLatestExchangeRate(ctx)
	if err == nil {
		change := 1.0 + (rand.Float64()*0.002 - 0.001)
		newRate := int64(float64(rate.RateAudUsd) * change)
		_ = q.UpsertExchangeRate(ctx, db.UpsertExchangeRateParams{
			Date:       today,
			RateAudUsd: newRate,
		})
	}

	slog.Debug("fake live tick", "stocks", updated, "date", today)
}
