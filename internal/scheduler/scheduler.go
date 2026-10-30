package scheduler

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
	"github.com/exploded/game/internal/media"
	"github.com/exploded/game/internal/notify"
)

var pricesChanged atomic.Bool
var quotesRunning atomic.Bool
var profilesRunning atomic.Bool

// quoteEveryMinutes is how often intraday quotes run during the US session.
// Each run costs 1 credit per S&P 500 stock (about 520), close to the
// Venture plan's 610 a minute, so every 2 minutes leaves room for other jobs.
const quoteEveryMinutes = 2

// SignalPricesChanged marks that new prices are available, triggering
// limit-order processing and portfolio revaluation on the next tick.
func SignalPricesChanged() { pricesChanged.Store(true) }

// Start launches the background scheduler that runs every minute.
// loc is the timezone used for game date comparisons (start/end dates).
// med runs the Learn jobs (video sync, topics, live stream emails).
func Start(ctx context.Context, q *db.Queries, rawDB *sql.DB, loc *time.Location, m *mail.Mailer, med *media.Service) {
	// Kick off historical backfill in the background if no prices exist yet.
	// Runs once at startup, independent of the hour gate used for daily updates.
	go startupBackfill(ctx, q, rawDB)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	slog.Info("scheduler started", "timezone", loc)
	if only := os.Getenv("TWELVEDATA_SYMBOLS"); only != "" {
		slog.Info("Twelve Data jobs limited by TWELVEDATA_SYMBOLS", "symbols", only)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("scheduler stopped")
			return
		case <-ticker.C:
			run(ctx, q, rawDB, loc, m)
			med.Run(ctx, time.Now())
		}
	}
}

func run(ctx context.Context, q *db.Queries, rawDB *sql.DB, loc *time.Location, m *mail.Mailer) {
	now := time.Now()
	today := now.In(loc).Format("2006-01-02")

	// 1. Make sure this quarter's game and the next exist, then advance game
	// statuses.
	market.EnsureQuarterlyGames(ctx, q, now.In(loc))
	q.AdvancePendingGames(ctx, today)
	q.FinishExpiredGames(ctx, today)

	// 2. Fetch prices (skip weekends; use UTC for market hours).
	utcNow := now.UTC()
	weekday := utcNow.Weekday()
	if weekday != time.Saturday && weekday != time.Sunday {
		maybeFetchPrices(ctx, q, rawDB, utcNow)

		// Refresh week-old company profiles after the daily prices, well
		// clear of the US session so it doesn't hold up intraday quotes.
		if utcNow.Hour() == 23 && utcNow.Minute() == 0 {
			go refreshProfiles(ctx, q, rawDB)
		}
	}
	maybeFetchQuotes(ctx, q, rawDB, now)

	// 3-4. Process limit orders and revalue portfolios only when prices changed.
	if pricesChanged.Swap(false) {
		slog.Info("prices changed, processing orders and revaluing portfolios")
		market.ProcessLimitOrders(ctx, q)
		if err := market.RevaluePortfolios(ctx, q); err != nil {
			slog.Error("revalue portfolios", "error", err)
		}
	}

	// 5. Refresh news from TickerTick (every 4 hours, on the hour).
	if utcNow.Minute() == 0 && utcNow.Hour()%4 == 0 {
		maybeFetchNews(ctx, q, rawDB)
	}

	// 6. Email order fills, game results and the like.
	notify.Run(ctx, q, m, now, loc)

	// 7. Clean expired sessions and sign-in requests.
	q.DeleteExpiredSessions(ctx)
	q.DeleteOldLoginRequests(ctx)
}

// startupBackfill runs FetchAll once at server startup if any sp500 stock is
// still missing price history. Uses ListStocksWithoutHistory (same query as
// FetchAll's backfill branch) so it correctly detects partial state from an
// interrupted previous run, not just a completely empty prices table.
// Then it fills in missing or stale company profiles, unless the US session is
// open: a full refresh takes most of the quota for nearly an hour, so during
// the session it's left to the 23:00 UTC run.
func startupBackfill(ctx context.Context, q *db.Queries, rawDB *sql.DB) {
	defer func() {
		if !inUSSession(time.Now()) {
			refreshProfiles(ctx, q, rawDB)
		}
	}()

	missing, err := q.ListStocksWithoutHistory(ctx, "sp500")
	if err != nil {
		slog.Error("startup backfill: list missing", "error", err)
		return
	}
	missing = market.LimitStocks(missing)
	if len(missing) == 0 {
		return // every stock already has at least one price row
	}
	slog.Info("S&P 500 backfill needed, starting in background", "missing", len(missing))
	fetcher := market.NewFetcher(q, rawDB)
	if err := fetcher.FetchAll(q); err != nil {
		slog.Error("startup backfill", "error", err)
	}
	SignalPricesChanged()
}

func maybeFetchPrices(ctx context.Context, q *db.Queries, rawDB *sql.DB, now time.Time) {
	hour := now.Hour()
	today := now.Format("2006-01-02")

	// S&P 500: fetch after 22:00 UTC (market closes ~21:00 UTC).
	if hour >= 22 {
		log, err := q.GetLatestFetchLog(ctx, "sp500")
		if err != nil || !isSameDay(log.CreatedAt, today) {
			slog.Info("fetching S&P 500 prices")
			fetcher := market.NewFetcher(q, rawDB)
			fetcher.FetchMarketOnly(q, "sp500")
			SignalPricesChanged()
		}
	}
}

// maybeFetchQuotes fetches intraday quotes every quoteEveryMinutes during the
// US regular session (9:30 to 16:00 New York time), plus 10 minutes after the
// close to pick up the final trades. Holidays need no calendar: FetchQuotes
// skips quotes that aren't dated today.
func maybeFetchQuotes(ctx context.Context, q *db.Queries, rawDB *sql.DB, now time.Time) {
	if !inUSSession(now) {
		return
	}
	ny := now.In(market.NewYork)
	if ny.Minute()%quoteEveryMinutes != 0 {
		return
	}
	if !quotesRunning.CompareAndSwap(false, true) {
		return // previous run still waiting for credits
	}
	go func() {
		defer quotesRunning.Store(false)
		fetcher := market.NewFetcher(q, rawDB)
		saved, err := fetcher.FetchQuotes(ctx, q, ny.Format("2006-01-02"))
		if err != nil {
			slog.Error("intraday quotes", "error", err)
		}
		if saved > 0 {
			SignalPricesChanged()
		}
	}()
}

// inUSSession reports whether now falls on a weekday between 9:30 and 16:10
// New York time: the regular session plus 10 minutes after the close. It
// doesn't know about holidays.
func inUSSession(now time.Time) bool {
	if market.NewYork == nil {
		return false
	}
	ny := now.In(market.NewYork)
	if ny.Weekday() == time.Saturday || ny.Weekday() == time.Sunday {
		return false
	}
	mins := ny.Hour()*60 + ny.Minute()
	return mins >= 9*60+30 && mins <= 16*60+10
}

// refreshProfiles updates missing or stale company profiles. Skips if a
// refresh is already running.
func refreshProfiles(ctx context.Context, q *db.Queries, rawDB *sql.DB) {
	if !profilesRunning.CompareAndSwap(false, true) {
		return
	}
	defer profilesRunning.Store(false)
	if err := market.NewFetcher(q, rawDB).RefreshProfiles(ctx, q); err != nil {
		slog.Error("profile refresh", "error", err)
	}
}

// maybeFetchNews refreshes stock news from TickerTick for S&P 500 stocks
// (TickerTick only knows US tickers; see market.TickerTickMarket).
// Runs in a background goroutine since the full cycle takes ~90 minutes
// due to rate limiting (10 req/min).
func maybeFetchNews(ctx context.Context, q *db.Queries, rawDB *sql.DB) {
	slog.Info("starting scheduled TickerTick news refresh")
	go func() {
		fetcher := market.NewFetcher(q, rawDB)
		if err := fetcher.FetchTickerTickNews(ctx, q, market.TickerTickMarket); err != nil {
			slog.Error("scheduled tickertick news refresh", "error", err)
		}
	}()
}

func isSameDay(timestamp, today string) bool {
	if len(timestamp) < 10 {
		return false
	}
	return timestamp[:10] == today
}
