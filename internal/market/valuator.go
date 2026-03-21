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

	// Get latest exchange rate.
	var rateAUDUSD int64 = 1000000
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

	for _, p := range participants {
		pHoldings, err := q.ListHoldings(ctx, p.ID)
		if err != nil {
			continue
		}

		var holdingsValue int64
		for _, h := range pHoldings {
			// Convert holding value to game's base currency.
			converted := convertCurrency(h.CurrentValue, h.Currency, p.BaseCurrency, rateAUDUSD)
			holdingsValue += converted
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

	slog.Info("portfolios revalued", "participants", len(participants))
	return nil
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
