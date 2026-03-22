package market

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/exploded/game/internal/db"
)

func nint64(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

func itoa(v int64) string {
	return strconv.FormatInt(v, 10)
}

func fmtCentsSimple(c int64) string {
	return fmt.Sprintf("$%.2f", float64(c)/100)
}

// ProcessLimitOrders checks all open limit orders and fills those where the price condition is met.
func ProcessLimitOrders(ctx context.Context, q *db.Queries) {
	// Expire old orders first.
	today := time.Now().UTC().Format("2006-01-02")
	_ = q.ExpireOldLimitOrders(ctx, sql.NullString{String: today, Valid: true})

	orders, err := q.ListAllOpenOrders(ctx)
	if err != nil {
		slog.Error("list open orders", "error", err)
		return
	}

	for _, order := range orders {
		latestPrice, err := q.GetLatestPrice(ctx, order.StockID)
		if err != nil || latestPrice.Close == 0 {
			continue
		}

		shouldFill := false
		if order.Type == "buy" && latestPrice.Close <= order.LimitPrice {
			shouldFill = true
		} else if order.Type == "sell" && latestPrice.Close >= order.LimitPrice {
			shouldFill = true
		}

		if !shouldFill {
			continue
		}

		// Fill the order by executing the trade.
		err = fillLimitOrder(ctx, q, order, latestPrice.Close)
		if err != nil {
			slog.Error("fill limit order", "orderID", order.ID, "error", err)
			continue
		}

		slog.Info("limit order filled", "orderID", order.ID, "symbol", order.Symbol, "type", order.Type, "qty", order.Quantity, "price", latestPrice.Close)
	}
}

func fillLimitOrder(ctx context.Context, q *db.Queries, order db.ListAllOpenOrdersRow, pricePerShare int64) error {
	participant, err := q.GetParticipant(ctx, order.ParticipantID)
	if err != nil {
		return err
	}

	game, err := q.GetGame(ctx, order.GameID)
	if err != nil {
		return err
	}

	if game.Status != "active" {
		_ = q.CancelLimitOrder(ctx, order.ID)
		return nil
	}

	totalNative := order.Quantity * pricePerShare

	// Get exchange rate.
	var exchangeRate int64 = 1000000
	if order.Currency != game.BaseCurrency {
		rate, err := q.GetLatestExchangeRate(ctx)
		if err == nil {
			exchangeRate = rate.RateAudUsd
		}
	}

	totalBase := convertCurrency(totalNative, order.Currency, game.BaseCurrency, exchangeRate)

	if order.Type == "buy" {
		cost := totalBase + game.TradeFee
		if cost > participant.CashBalance {
			// Can't afford - cancel.
			_ = q.CancelLimitOrder(ctx, order.ID)
			return nil
		}

		// Deduct cash.
		_ = q.UpdateCashBalance(ctx, db.UpdateCashBalanceParams{
			CashBalance: participant.CashBalance - cost,
			ID:          participant.ID,
		})

		// Upsert holding.
		existing, _ := q.GetHolding(ctx, db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: order.StockID,
		})
		newQty := existing.Quantity + order.Quantity
		var newAvg int64
		if existing.ID != 0 {
			newAvg = ((existing.Quantity * existing.AvgCost) + (order.Quantity * pricePerShare)) / newQty
		} else {
			newAvg = pricePerShare
		}
		_ = q.UpsertHolding(ctx, db.UpsertHoldingParams{
			ParticipantID: participant.ID,
			StockID:       order.StockID,
			Quantity:      newQty,
			AvgCost:       newAvg,
			CurrentValue:  newQty * pricePerShare,
		})

		// Insert transaction.
		_, _ = q.InsertTransaction(ctx, db.InsertTransactionParams{
			ParticipantID:    participant.ID,
			StockID:          order.StockID,
			Type:             "buy",
			Quantity:         order.Quantity,
			Price:            pricePerShare,
			Total:            totalNative,
			ConvertedTotal:   totalBase,
			ExchangeRateUsed: exchangeRate,
			Fee:              game.TradeFee,
		})

	} else { // sell
		holding, err := q.GetHolding(ctx, db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: order.StockID,
		})
		if err != nil || holding.Quantity < order.Quantity {
			_ = q.CancelLimitOrder(ctx, order.ID)
			return nil
		}

		proceeds := totalBase - game.TradeFee
		if proceeds < 0 {
			proceeds = 0
		}

		_ = q.UpdateCashBalance(ctx, db.UpdateCashBalanceParams{
			CashBalance: participant.CashBalance + proceeds,
			ID:          participant.ID,
		})

		newQty := holding.Quantity - order.Quantity
		if newQty == 0 {
			_ = q.DeleteHolding(ctx, db.DeleteHoldingParams{
				ParticipantID: participant.ID, StockID: order.StockID,
			})
		} else {
			_ = q.UpsertHolding(ctx, db.UpsertHoldingParams{
				ParticipantID: participant.ID,
				StockID:       order.StockID,
				Quantity:      newQty,
				AvgCost:       holding.AvgCost,
				CurrentValue:  newQty * pricePerShare,
			})
		}

		_, _ = q.InsertTransaction(ctx, db.InsertTransactionParams{
			ParticipantID:    participant.ID,
			StockID:          order.StockID,
			Type:             "sell",
			Quantity:         order.Quantity,
			Price:            pricePerShare,
			Total:            totalNative,
			ConvertedTotal:   totalBase,
			ExchangeRateUsed: exchangeRate,
			Fee:              game.TradeFee,
		})
	}

	// Mark filled.
	_ = q.FillLimitOrder(ctx, order.ID)

	// Create notification.
	// Find user_id from participant.
	_ = q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  participant.UserID,
		GameID:  nint64(order.GameID),
		Type:    "order_filled",
		Title:   "Limit Order Filled",
		Message: order.Type + " " + itoa(order.Quantity) + " shares of " + order.Symbol + " filled at " + fmtCentsSimple(pricePerShare),
	})

	return nil
}

