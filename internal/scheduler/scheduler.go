package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// Start launches the background scheduler that runs every minute.
// loc is the timezone used for game date comparisons (start/end dates).
func Start(ctx context.Context, q *db.Queries, loc *time.Location) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	slog.Info("scheduler started", "timezone", loc)

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return
		case <-ticker.C:
			run(ctx, q, loc)
		}
	}
}

func run(ctx context.Context, q *db.Queries, loc *time.Location) {
	now := time.Now()
	today := now.In(loc).Format("2006-01-02")

	// 1. Advance game statuses.
	q.AdvancePendingGames(ctx, today)
	q.FinishExpiredGames(ctx, today)

	// 2. Fetch prices (skip weekends; use UTC for market hours).
	utcNow := now.UTC()
	weekday := utcNow.Weekday()
	if weekday != time.Saturday && weekday != time.Sunday {
		maybeFetchPrices(ctx, q, utcNow)
	}

	// 3. Process limit orders (after prices are available).
	market.ProcessLimitOrders(ctx, q)

	// 4. Revalue portfolios (runs after any price fetch and order fills).
	if err := market.RevaluePortfolios(ctx, q); err != nil {
		slog.Error("revalue portfolios", "error", err)
	}

	// 5. Process recurring games.
	market.ProcessRecurringGames(ctx, q)

	// 6. Clean expired sessions.
	q.DeleteExpiredSessions(ctx)
}

func maybeFetchPrices(ctx context.Context, q *db.Queries, now time.Time) {
	hour := now.Hour()
	today := now.Format("2006-01-02")

	fetcher := market.NewFetcher(q)

	// ASX: fetch after 07:00 UTC (market closes ~06:00 UTC).
	if hour >= 7 {
		log, err := q.GetLatestFetchLog(ctx, "asx")
		if err != nil || !isSameDay(log.CreatedAt, today) {
			slog.Info("fetching ASX prices")
			fetcher.FetchMarketOnly(q, "asx")
		}
	}

	// S&P 500: fetch after 22:00 UTC (market closes ~21:00 UTC).
	if hour >= 22 {
		log, err := q.GetLatestFetchLog(ctx, "sp500")
		if err != nil || !isSameDay(log.CreatedAt, today) {
			slog.Info("fetching S&P 500 prices")
			fetcher.FetchMarketOnly(q, "sp500")
		}

		// Forex: fetch with S&P.
		fxLog, err := q.GetLatestFetchLog(ctx, "forex")
		if err != nil || !isSameDay(fxLog.CreatedAt, today) {
			slog.Info("fetching forex rate")
			fetcher.FetchForexOnly(q)
		}
	}
}

func isSameDay(timestamp, today string) bool {
	if len(timestamp) < 10 {
		return false
	}
	return timestamp[:10] == today
}
