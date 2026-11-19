package market

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/exploded/game/internal/db"
)

// RevaluePortfolios updates current_value on all holdings in active games,
// recalculates participant portfolio_value, and takes daily snapshots.
//
// Holdings are valued one stock at a time from the quantity held at that
// moment, and each player's cash and holdings are read and their value
// written in one short transaction. Pairing cash read at the start with
// holdings read later overstated anyone who traded while it ran (found in
// review, 19 November 2026).
func RevaluePortfolios(ctx context.Context, q *db.Queries, rawDB *sql.DB) error {
	stocks, err := q.ListActiveHeldStocks(ctx)
	if err != nil {
		return err
	}

	// Get latest exchange rate. Without one (0), a portfolio holding stocks in
	// another currency keeps its last value rather than convert 1:1.
	var rateAUDUSD int64
	rate, err := q.GetLatestExchangeRate(ctx)
	if err == nil {
		rateAUDUSD = rate.RateAudUsd
	}

	// Value every holding of each stock at its latest price.
	for _, stockID := range stocks {
		latest, err := q.GetLatestPrice(ctx, stockID)
		if err != nil || latest.Close <= 0 {
			continue
		}
		if err := q.RevalueStockHoldings(ctx, db.RevalueStockHoldingsParams{
			Price: ExDividendClose(ctx, q, stockID, latest), StockID: stockID,
		}); err != nil {
			return err
		}
	}

	// Recalculate portfolio values for all active participants.
	participants, err := q.ListActiveParticipants(ctx)
	if err != nil {
		return err
	}

	today := time.Now().UTC().Format("2006-01-02")

	skipped, failed := 0, 0
	for _, p := range participants {
		ok, err := revalueOne(ctx, q, rawDB, p.ID, p.BaseCurrency, rateAUDUSD, today)
		if err != nil {
			failed++
			slog.Error("revalue participant", "participant", p.ID, "error", err)
			continue
		}
		if !ok {
			skipped++
		}
	}

	if skipped > 0 {
		slog.Warn("no exchange rate, portfolios left at their last value", "participants", skipped)
	}
	slog.Info("portfolios revalued", "participants", len(participants)-skipped-failed)
	if failed > 0 {
		return fmt.Errorf("%d portfolios not revalued", failed)
	}
	return nil
}

// RevalueParticipant revalues one player straight after a trade, so their
// summary is right without waiting for the next RevaluePortfolios. Holdings
// keep the values they have.
func RevalueParticipant(ctx context.Context, q *db.Queries, rawDB *sql.DB, participantID int64, baseCurrency string) error {
	var rateAUDUSD int64
	if r, err := q.GetLatestExchangeRate(ctx); err == nil {
		rateAUDUSD = r.RateAudUsd
	}
	_, err := revalueOne(ctx, q, rawDB, participantID, baseCurrency, rateAUDUSD, time.Now().UTC().Format("2006-01-02"))
	return err
}

// revalueOne writes one player's portfolio value and today's snapshot from
// their cash and holdings as they are now. It's false, writing nothing, when
// a holding needs an exchange rate and there isn't one.
func revalueOne(ctx context.Context, q *db.Queries, rawDB *sql.DB, participantID int64, baseCurrency string, rateAUDUSD int64, today string) (bool, error) {
	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	p, err := qtx.GetParticipant(ctx, participantID)
	if err != nil {
		return false, err
	}
	holdings, err := qtx.ListHoldings(ctx, participantID)
	if err != nil {
		return false, err
	}
	holdingsValue, ok := HoldingsValue(holdings, baseCurrency, rateAUDUSD)
	if !ok {
		return false, nil
	}
	totalValue := p.CashBalance + holdingsValue
	if err := qtx.UpdatePortfolioValue(ctx, db.UpdatePortfolioValueParams{
		PortfolioValue: totalValue, ID: p.ID,
	}); err != nil {
		return false, err
	}
	if err := qtx.UpsertSnapshot(ctx, db.UpsertSnapshotParams{
		ParticipantID: p.ID,
		Date:          today,
		CashBalance:   p.CashBalance,
		HoldingsValue: holdingsValue,
		TotalValue:    totalValue,
	}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

// HoldingsValue totals holdings' current values in the game's base currency.
// It's false when a holding is in another currency and there's no rate
// (rateAUDUSD 0): converting 1:1 would misvalue the portfolio.
func HoldingsValue(holdings []db.ListHoldingsRow, baseCurrency string, rateAUDUSD int64) (int64, bool) {
	var total int64
	for _, h := range holdings {
		if h.Currency != baseCurrency && rateAUDUSD == 0 {
			return 0, false
		}
		total += convertCurrency(h.CurrentValue, h.Currency, baseCurrency, rateAUDUSD)
	}
	return total, true
}

func convertCurrency(amount int64, from, to string, rateAUDUSD int64) int64 {
	if from == to {
		return amount
	}
	if from == "AUD" && to == "USD" {
		return (amount * rateAUDUSD) / 1000000
	}
	if from == "USD" && to == "AUD" {
		return (amount * 1000000) / rateAUDUSD
	}
	return amount
}
