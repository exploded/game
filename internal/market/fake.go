package market

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"time"

	"github.com/exploded/game/internal/db"
)

// SeedFakePrices generates 30 days of realistic random price data for all active stocks.
// Only available in dev mode.
func SeedFakePrices(ctx context.Context, q *db.Queries) int {
	stocks, err := q.ListStocksByMarket(ctx, db.ListStocksByMarketParams{
		Market: "both", Column2: "both",
	})
	if err != nil {
		slog.Error("seed fake prices: list stocks", "error", err)
		return 0
	}

	today := time.Now().UTC()
	count := 0

	for _, s := range stocks {
		// Random base price between $5 and $500.
		basePrice := 500 + rand.IntN(49500) // cents: $5.00 to $500.00

		price := int64(basePrice)
		for day := 30; day >= 0; day-- {
			d := today.AddDate(0, 0, -day)
			// Skip weekends.
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			date := d.Format("2006-01-02")

			// Random daily movement: -3% to +3%.
			change := 1.0 + (rand.Float64()*0.06 - 0.03)
			price = int64(float64(price) * change)
			if price < 100 {
				price = 100 // floor at $1.00
			}

			// Generate OHLC from close.
			spread := int64(float64(price) * 0.02) // 2% spread
			open := price + rand.Int64N(spread) - spread/2
			high := price + rand.Int64N(spread)
			low := price - rand.Int64N(spread)
			if high < open {
				high = open
			}
			if high < price {
				high = price
			}
			if low > open {
				low = open
			}
			if low > price {
				low = price
			}
			vol := int64(100000 + rand.IntN(10000000))

			q.UpsertPrice(ctx, db.UpsertPriceParams{
				StockID: s.ID,
				Date:    date,
				Open:    open,
				High:    high,
				Low:     low,
				Close:   price,
				Volume:  vol,
			})
		}
		count++
	}

	// Also seed a fake exchange rate.
	for day := 30; day >= 0; day-- {
		d := today.AddDate(0, 0, -day)
		if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
			continue
		}
		// AUD/USD roughly 0.62-0.67
		rate := 620000 + rand.Int64N(50000)
		q.UpsertExchangeRate(ctx, db.UpsertExchangeRateParams{
			Date:       d.Format("2006-01-02"),
			RateAudUsd: rate,
		})
	}

	slog.Info("fake prices seeded", "stocks", count, "days", 30)
	return count
}
