package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/exploded/game/internal/backup"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
	"github.com/exploded/game/internal/media"
	"github.com/exploded/game/internal/notify"
)

var pricesChanged atomic.Bool
var quotesRunning atomic.Bool
var profilesRunning atomic.Bool
var ratesRunning atomic.Bool
var newsRunning atomic.Bool
var splitsRunning atomic.Bool
var backupRunning atomic.Bool

// splitsCheckedFor is the New York date of the last good splits check, and
// splitsTriedAt when the last one started (Unix seconds).
var splitsCheckedFor atomic.Value
var splitsTriedAt atomic.Int64

// backupTriedAt is when the last database backup started (Unix seconds).
var backupTriedAt atomic.Int64

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
// bk uploads the daily database backup; nil means backups are off.
func Start(ctx context.Context, q *db.Queries, rawDB *sql.DB, loc *time.Location, m *mail.Mailer, med *media.Service, bk *backup.Uploader) {
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
			maybeBackup(ctx, q, bk, time.Now().In(loc))
		}
	}
}

func run(ctx context.Context, q *db.Queries, rawDB *sql.DB, loc *time.Location, m *mail.Mailer) {
	now := time.Now()
	today := now.In(loc).Format("2006-01-02")

	// 1. Make sure this month's game and the next exist, pay yesterday's
	// interest on cash, then advance game statuses. Interest comes first so a
	// game's last day is paid before the game finishes. A game that finishes
	// gives Champion and Podium Finish to its top players.
	market.EnsureMonthlyGames(ctx, q, now.In(loc))
	if err := market.CreditInterest(ctx, q, rawDB, now.In(loc)); err != nil {
		slog.Error("credit interest", "error", err)
	}
	q.AdvancePendingGames(ctx, today)
	if err := q.CancelOrdersInExpiredGames(ctx, today); err != nil {
		slog.Error("cancel orders in ended games", "error", err)
	}
	finished, err := q.FinishExpiredGames(ctx, today)
	if err != nil {
		slog.Error("finish expired games", "error", err)
	}
	for _, g := range finished {
		market.AwardPlaces(ctx, q, g)
	}

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
	maybeCheckSplits(ctx, q, rawDB, now)
	if utcNow.Minute()%15 == 0 {
		go fetchRates(ctx, q, rawDB)
	}

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

// startupBackfill fetches the history of any S&P 500 stock that hasn't had it
// yet (market.Backfill), which also resumes an interrupted backfill.
// Then it fills in missing or stale company profiles, unless the US session is
// open: a full refresh takes most of the quota for nearly an hour, so during
// the session it's left to the 23:00 UTC run.
func startupBackfill(ctx context.Context, q *db.Queries, rawDB *sql.DB) {
	// Before anything else: until a rate is stored, trades in a stock priced
	// in another currency are refused.
	fetchRates(ctx, q, rawDB)

	defer func() {
		if !inUSSession(time.Now()) {
			refreshProfiles(ctx, q, rawDB)
		}
	}()

	if err := market.NewFetcher(q, rawDB).Backfill(ctx, q); err != nil {
		slog.Error("startup backfill", "error", err)
	}
	SignalPricesChanged()
}

// maybeCheckSplits applies stock splits (market.CheckSplits) once each New
// York weekday, from 10:00 UTC: after the night's daily fetch and before
// the 13:30 or 14:30 UTC open, so holdings, orders and prices change
// together. A failed check is tried again after dailyRetry.
func maybeCheckSplits(ctx context.Context, q *db.Queries, rawDB *sql.DB, now time.Time) {
	if market.NewYork == nil || now.UTC().Hour() < 10 {
		return
	}
	ny := now.In(market.NewYork)
	if ny.Weekday() == time.Saturday || ny.Weekday() == time.Sunday {
		return
	}
	today := ny.Format(time.DateOnly)
	if done, _ := splitsCheckedFor.Load().(string); done == today {
		return
	}
	if now.Sub(time.Unix(splitsTriedAt.Load(), 0)) < dailyRetry {
		return
	}
	if !splitsRunning.CompareAndSwap(false, true) {
		return
	}
	splitsTriedAt.Store(now.Unix())
	go func() {
		defer splitsRunning.Store(false)
		n, err := market.NewFetcher(q, rawDB).CheckSplits(ctx, q, today)
		if err != nil {
			slog.Error("stock splits", "error", err)
			return
		}
		splitsCheckedFor.Store(today)
		if n > 0 {
			SignalPricesChanged()
		}
	}()
}

// backupHour is the hour in TIMEZONE from which the daily backup runs.
const backupHour = 3

// backupRetry is how long after a failed backup it's tried again. Each
// failure logs an error, which monitor sends to Discord, so not every minute.
const backupRetry = time.Hour

// backupDayKey is the media_state row holding the date of the last good
// backup, so a restart or deploy doesn't upload that day's backup again.
const backupDayKey = "backup_day"

// maybeBackup uploads a snapshot of the database to S3 once a day, from
// backupHour in TIMEZONE (now is in it). It runs in the background: a
// 300 MB database takes a minute or so to copy, compress and upload.
func maybeBackup(ctx context.Context, q *db.Queries, bk *backup.Uploader, now time.Time) {
	if bk == nil || now.Hour() < backupHour {
		return
	}
	today := now.Format(time.DateOnly)
	if done, _ := q.GetMediaState(ctx, backupDayKey); done == today {
		return
	}
	if now.Sub(time.Unix(backupTriedAt.Load(), 0)) < backupRetry {
		return
	}
	if !backupRunning.CompareAndSwap(false, true) {
		return
	}
	backupTriedAt.Store(now.Unix())
	go func() {
		defer backupRunning.Store(false)
		start := time.Now()
		size, err := bk.Upload(ctx, today)
		if err != nil {
			slog.Error("database backup", "error", err)
			return
		}
		if err := q.SetMediaState(ctx, db.SetMediaStateParams{Key: backupDayKey, Value: today}); err != nil {
			slog.Error("database backup: record day", "error", err)
		}
		slog.Info("database backup uploaded", "bucket", bk.Bucket(), "key", backup.Key(today),
			"bytes", size, "took", time.Since(start).Round(time.Second))
	}()
}

// dailyRetry is how long after a daily fetch that saved nothing it's tried
// again, so an outage doesn't spend a whole fetch's credits every minute.
const dailyRetry = 15 * time.Minute

// dailyFetchDue says whether the daily S&P 500 fetch should run at now
// (UTC): from 22:00 (the market closes at 20:00 or 21:00 UTC) until a daily
// run that day has saved prices. last is the newest daily run, if found. A
// backfill doesn't count: its rows have mode 'backfill'.
func dailyFetchDue(last db.PriceFetchLog, found bool, now time.Time) bool {
	if now.Hour() < 22 {
		return false
	}
	if !found || !isSameDay(last.CreatedAt, now.Format(time.DateOnly)) {
		return true
	}
	if last.SymbolsOk > 0 {
		return false
	}
	at, err := time.Parse(time.DateTime, last.CreatedAt)
	return err != nil || now.Sub(at) >= dailyRetry
}

// maybeFetchPrices runs the daily fetch when it's due. It runs in the
// scheduler's loop, so the next minute's check waits until it's logged.
func maybeFetchPrices(ctx context.Context, q *db.Queries, rawDB *sql.DB, now time.Time) {
	last, err := q.GetLastDailyFetch(ctx, "sp500")
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Error("checking daily fetch", "error", err)
		return
	}
	if !dailyFetchDue(last, err == nil, now) {
		return
	}
	slog.Info("fetching S&P 500 prices")
	market.NewFetcher(q, rawDB).FetchMarketOnly(q, "sp500")
	SignalPricesChanged()
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

// fetchRates updates the AUD/USD rate. Skips if a fetch is already running.
func fetchRates(ctx context.Context, q *db.Queries, rawDB *sql.DB) {
	if !ratesRunning.CompareAndSwap(false, true) {
		return
	}
	defer ratesRunning.Store(false)
	if err := market.NewFetcher(q, rawDB).FetchExchangeRates(ctx, q); err != nil {
		slog.Error("exchange rate", "error", err)
	}
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
	if !newsRunning.CompareAndSwap(false, true) {
		slog.Warn("previous TickerTick news refresh still running, skipping")
		return
	}
	slog.Info("starting scheduled TickerTick news refresh")
	go func() {
		defer newsRunning.Store(false)
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
