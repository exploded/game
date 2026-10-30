package market

import (
	"context"
	"log/slog"
	"time"

	"github.com/exploded/game/internal/db"
)

// RevaluePortfolios updates current_value on all holdings in active games,
// recalculates participant portfolio_value, and takes daily snapshots.
func RevaluePortfolios(ctx context.Context, q *db.Queries) error {
	holdings, err := q.ListAllActiveHoldings(ctx)
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

	// Update each holding's current_value.
	for _, h := range holdings {
		latest, err := q.GetLatestPrice(ctx, h.StockID)
		if err != nil {
			continue
		}
		currentValue := h.Quantity * latest.Close
		q.UpdateHoldingValue(ctx, db.UpdateHoldingValueParams{
			CurrentValue: currentValue,
			ID:           h.ID,
		})
	}

	// Recalculate portfolio values for all active participants.
	participants, err := q.ListActiveParticipants(ctx)
	if err != nil {
		return err
	}

	today := time.Now().UTC().Format("2006-01-02")

	skipped := 0
	for _, p := range participants {
		pHoldings, err := q.ListHoldings(ctx, p.ID)
		if err != nil {
			continue
		}

		holdingsValue, ok := HoldingsValue(pHoldings, p.BaseCurrency, rateAUDUSD)
		if !ok {
			skipped++
			continue
		}

		totalValue := p.CashBalance + holdingsValue
		q.UpdatePortfolioValue(ctx, db.UpdatePortfolioValueParams{
			PortfolioValue: totalValue,
			ID:             p.ID,
		})

		// Daily snapshot.
		q.UpsertSnapshot(ctx, db.UpsertSnapshotParams{
			ParticipantID: p.ID,
			Date:          today,
			CashBalance:   p.CashBalance,
			HoldingsValue: holdingsValue,
			TotalValue:    totalValue,
		})
	}

	if skipped > 0 {
		slog.Warn("no exchange rate, portfolios left at their last value", "participants", skipped)
	}
	slog.Info("portfolios revalued", "participants", len(participants)-skipped)
	return nil
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
