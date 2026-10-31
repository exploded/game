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

// MarketClosed reports whether a stock's market is closed, so its last stored
// price is stale. Only S&P 500 stocks get intraday quotes, so only they can be
// closed, and only while QuoteStatus says the US market is closed.
func MarketClosed(stockMarket string) bool {
	if stockMarket != "sp500" || NewYork == nil {
		return false
	}
	_, open := QuoteStatus()
	return !open
}

// ProcessLimitOrders fills open limit orders whose price condition is met, and
// queued market orders, once the market has opened. Neither fills against the
// last price while the market is closed.
func ProcessLimitOrders(ctx context.Context, q *db.Queries) {
	// Expire old orders first, and tell their owners.
	today := time.Now().UTC().Format("2006-01-02")
	expired, _ := q.ExpireOldLimitOrders(ctx, sql.NullString{String: today, Valid: true})
	for _, o := range expired {
		notifyExpired(ctx, q, o)
	}

	// Orders fill at prices from the session: the market must be open and an
	// S&P 500 stock's newest price dated today.
	sessionDate := ""
	if NewYork != nil {
		sessionDate = time.Now().In(NewYork).Format("2006-01-02")
	}

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

		if MarketClosed(order.Market) || (order.Market == "sp500" && sessionDate != "" && latestPrice.Date != sessionDate) {
			continue
		}

		shouldFill := false
		if order.Kind == "market" {
			shouldFill = true
		} else if order.Type == "buy" && latestPrice.Close <= order.LimitPrice {
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
		cancelOrder(ctx, q, order, participant.UserID, "the game is no longer active")
		return nil
	}

	totalNative := order.Quantity * pricePerShare

	// Get exchange rate. Without one, leave the order for a later run rather
	// than convert 1:1.
	var exchangeRate int64 = 1000000
	if order.Currency != game.BaseCurrency {
		rate, err := q.GetLatestExchangeRate(ctx)
		if err != nil {
			return fmt.Errorf("no exchange rate: %w", err)
		}
		exchangeRate = rate.RateAudUsd
	}

	totalBase := convertCurrency(totalNative, order.Currency, game.BaseCurrency, exchangeRate)
	fee := TradeFees(game, order.Currency, totalBase)

	if order.Type == "buy" {
		cost := totalBase + fee
		if cost > participant.CashBalance {
			cancelOrder(ctx, q, order, participant.UserID, "you didn't have enough cash at "+fmtCentsSimple(pricePerShare))
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
		newQty, newCost := Fill(existing.Quantity, existing.CostBasis, "buy", order.Quantity, pricePerShare)
		if newQty == 0 {
			_ = q.DeleteHolding(ctx, db.DeleteHoldingParams{
				ParticipantID: participant.ID, StockID: order.StockID,
			})
		} else {
			_ = q.UpsertHolding(ctx, db.UpsertHoldingParams{
				ParticipantID: participant.ID,
				StockID:       order.StockID,
				Quantity:      newQty,
				AvgCost:       AvgCost(newQty, newCost),
				CostBasis:     newCost,
				CurrentValue:  newQty * pricePerShare,
			})
		}

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
			Fee:              fee,
		})

	} else { // sell
		// No holding reads as zero shares. A queued market sell can go short
		// in a short-selling game, as it could have if placed while open.
		holding, _ := q.GetHolding(ctx, db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: order.StockID,
		})
		canShort := order.Kind == "market" && game.AllowShort != 0
		if holding.Quantity < order.Quantity && !canShort {
			cancelOrder(ctx, q, order, participant.UserID, "you no longer hold enough shares")
			return nil
		}

		proceeds := totalBase - fee
		if proceeds < 0 {
			proceeds = 0
		}

		_ = q.UpdateCashBalance(ctx, db.UpdateCashBalanceParams{
			CashBalance: participant.CashBalance + proceeds,
			ID:          participant.ID,
		})

		newQty, newCost := Fill(holding.Quantity, holding.CostBasis, "sell", order.Quantity, pricePerShare)
		if newQty == 0 {
			_ = q.DeleteHolding(ctx, db.DeleteHoldingParams{
				ParticipantID: participant.ID, StockID: order.StockID,
			})
		} else {
			_ = q.UpsertHolding(ctx, db.UpsertHoldingParams{
				ParticipantID: participant.ID,
				StockID:       order.StockID,
				Quantity:      newQty,
				AvgCost:       AvgCost(newQty, newCost),
				CostBasis:     newCost,
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
			Fee:              fee,
		})
	}

	// Mark filled.
	_ = q.FillLimitOrder(ctx, order.ID)

	title, when := "Limit Order Filled", ""
	if order.Kind == "market" {
		title, when = "Queued Order Filled", " at the open"
	}
	_ = q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  participant.UserID,
		GameID:  nint64(order.GameID),
		Type:    "order_filled",
		Title:   title,
		Message: order.Type + " " + itoa(order.Quantity) + " shares of " + order.Symbol + " filled" + when + " at " + fmtCentsSimple(pricePerShare),
	})

	// The game's activity feed, as trade.go does for a trade that fills
	// straight away.
	verb, how := "bought", " with a limit order"
	if order.Type == "sell" {
		verb = "sold"
	}
	if order.Kind == "market" {
		how = " at the open"
	}
	noun := " shares of "
	if order.Quantity == 1 {
		noun = " share of "
	}
	_ = q.InsertActivity(ctx, db.InsertActivityParams{
		GameID: order.GameID,
		UserID: participant.UserID,
		Action: order.Type,
		Detail: verb + " " + itoa(order.Quantity) + noun + order.Symbol + how,
	})

	return nil
}

// notifyExpired tells the player their limit order expired unfilled.
func notifyExpired(ctx context.Context, q *db.Queries, o db.ExpireOldLimitOrdersRow) {
	participant, err := q.GetParticipant(ctx, o.ParticipantID)
	if err != nil {
		return
	}
	stock, err := q.GetStock(ctx, o.StockID)
	if err != nil {
		return
	}
	_ = q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  participant.UserID,
		GameID:  nint64(participant.GameID),
		Type:    "order_expired",
		Title:   "Limit Order Expired",
		Message: "Your " + o.Type + " order for " + itoa(o.Quantity) + " shares of " + stock.Symbol + " at " + fmtCentsSimple(o.LimitPrice) + " expired without filling.",
	})
}

// cancelOrder cancels an order that can't fill and tells the player why.
func cancelOrder(ctx context.Context, q *db.Queries, order db.ListAllOpenOrdersRow, userID int64, reason string) {
	_ = q.CancelLimitOrder(ctx, order.ID)
	title := "Limit Order Cancelled"
	if order.Kind == "market" {
		title = "Queued Order Cancelled"
	}
	_ = q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  userID,
		GameID:  nint64(order.GameID),
		Type:    "order_cancelled",
		Title:   title,
		Message: "Your " + order.Type + " order for " + itoa(order.Quantity) + " shares of " + order.Symbol + " was cancelled: " + reason + ".",
	})
}
