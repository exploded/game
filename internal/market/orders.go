package market

import (
	"context"
	"database/sql"
	"errors"
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
// last price while the market is closed. Each fill is its own transaction.
func ProcessLimitOrders(ctx context.Context, q *db.Queries, rawDB *sql.DB) {
	// Expire old orders first, and tell their owners.
	today := time.Now().UTC().Format("2006-01-02")
	expired, err := q.ExpireOldLimitOrders(ctx, sql.NullString{String: today, Valid: true})
	if err != nil {
		slog.Error("expire limit orders", "error", err)
	}
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

	prices := map[int64]db.StockPrice{} // one lookup per stock
	for _, order := range orders {
		latestPrice, seen := prices[order.StockID]
		if !seen {
			latestPrice, err = q.GetLatestPrice(ctx, order.StockID)
			if err != nil {
				latestPrice = db.StockPrice{}
			}
			prices[order.StockID] = latestPrice
		}
		if latestPrice.Close <= 0 {
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

		filled, err := fillLimitOrder(ctx, q, rawDB, order, latestPrice.Close)
		if err != nil {
			slog.Error("fill limit order", "orderID", order.ID, "error", err)
			continue
		}
		if filled {
			slog.Info("limit order filled", "orderID", order.ID, "symbol", order.Symbol, "type", order.Type, "qty", order.Quantity, "price", latestPrice.Close)
		}
	}
}

// fillLimitOrder fills one order at pricePerShare, or cancels it if it can't
// fill, in one transaction. It first claims the order (open → filled), so an
// order the player cancelled after ListAllOpenOrders read it is left alone,
// and everything it reads is read inside the transaction, so a trade or a
// dividend committed in between isn't overwritten. Any error rolls the whole
// fill back and leaves the order open for the next run. It reports whether
// the order filled.
func fillLimitOrder(ctx context.Context, q *db.Queries, rawDB *sql.DB, order db.ListAllOpenOrdersRow, pricePerShare int64) (bool, error) {
	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	qtx := q.WithTx(tx)

	claimed, err := qtx.ClaimOpenOrder(ctx, order.ID)
	if err != nil {
		return false, err
	}
	if claimed == 0 {
		return false, nil // cancelled, expired or filled since it was listed
	}

	participant, err := qtx.GetParticipant(ctx, order.ParticipantID)
	if err != nil {
		return false, err
	}
	game, err := qtx.GetGame(ctx, order.GameID)
	if err != nil {
		return false, err
	}
	stock, err := qtx.GetStock(ctx, order.StockID)
	if err != nil {
		return false, err
	}

	cancel := func(reason string) (bool, error) {
		if err := cancelOrder(ctx, qtx, order, participant.UserID, reason); err != nil {
			return false, err
		}
		return false, tx.Commit()
	}
	if game.Status != "active" {
		return cancel("the game is no longer active")
	}
	if stock.IsActive == 0 {
		return cancel(order.Symbol + " is no longer traded on The Investor Project")
	}
	totalNative, ok := TradeValue(order.Quantity, pricePerShare)
	if !ok {
		return cancel("the order was too large to fill")
	}

	// Without an exchange rate, leave the order for a later run rather than
	// convert 1:1.
	var exchangeRate int64 = 1000000
	if order.Currency != game.BaseCurrency {
		rate, err := qtx.GetLatestExchangeRate(ctx)
		if err != nil {
			return false, fmt.Errorf("no exchange rate: %w", err)
		}
		exchangeRate = rate.RateAudUsd
	}
	totalBase := convertCurrency(totalNative, order.Currency, game.BaseCurrency, exchangeRate)
	fee := TradeFees(game, order.Currency, totalBase)

	holding, err := qtx.GetHolding(ctx, db.GetHoldingParams{
		ParticipantID: participant.ID, StockID: order.StockID,
	})
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	var cash int64
	if order.Type == "buy" {
		cash = participant.CashBalance - (totalBase + fee)
		if cash < 0 {
			return cancel("you didn't have enough cash at " + fmtCentsSimple(pricePerShare))
		}
	} else {
		// No holding reads as zero shares. A queued market sell can go short
		// in a short-selling game, as it could have if placed while open.
		canShort := order.Kind == "market" && game.AllowShort != 0
		if holding.Quantity < order.Quantity && !canShort {
			return cancel("you no longer hold enough shares")
		}
		if holding.Quantity < order.Quantity {
			ok, err := ShortAllowed(ctx, qtx, participant, game.BaseCurrency, stock, holding.Quantity-order.Quantity, pricePerShare)
			if err != nil {
				return false, err
			}
			if !ok {
				return cancel("the short would be bigger than your portfolio")
			}
		}
		cash = participant.CashBalance + max(totalBase-fee, 0)
	}

	if err := qtx.UpdateCashBalance(ctx, db.UpdateCashBalanceParams{CashBalance: cash, ID: participant.ID}); err != nil {
		return false, err
	}
	newQty, newCost := Fill(holding.Quantity, holding.CostBasis, order.Type, order.Quantity, pricePerShare)
	if newQty == 0 {
		err = qtx.DeleteHolding(ctx, db.DeleteHoldingParams{ParticipantID: participant.ID, StockID: order.StockID})
	} else {
		err = qtx.UpsertHolding(ctx, db.UpsertHoldingParams{
			ParticipantID: participant.ID,
			StockID:       order.StockID,
			Quantity:      newQty,
			AvgCost:       AvgCost(newQty, newCost),
			CostBasis:     newCost,
			CurrentValue:  newQty * pricePerShare,
		})
	}
	if err != nil {
		return false, err
	}
	if _, err := qtx.InsertTransaction(ctx, db.InsertTransactionParams{
		ParticipantID:    participant.ID,
		StockID:          order.StockID,
		Type:             order.Type,
		Quantity:         order.Quantity,
		Price:            pricePerShare,
		Total:            totalNative,
		ConvertedTotal:   totalBase,
		ExchangeRateUsed: exchangeRate,
		Fee:              fee,
	}); err != nil {
		return false, err
	}

	title, when := "Limit Order Filled", ""
	if order.Kind == "market" {
		title, when = "Queued Order Filled", " at the open"
	}
	if err := qtx.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  participant.UserID,
		GameID:  nint64(order.GameID),
		Type:    "order_filled",
		Title:   title,
		Message: order.Type + " " + itoa(order.Quantity) + " shares of " + order.Symbol + " filled" + when + " at " + fmtCentsSimple(pricePerShare),
	}); err != nil {
		return false, err
	}

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
	if err := qtx.InsertActivity(ctx, db.InsertActivityParams{
		GameID:  order.GameID,
		UserID:  participant.UserID,
		Action:  order.Type,
		Detail:  verb + " " + itoa(order.Quantity) + noun + order.Symbol + how,
		StockID: sql.NullInt64{Int64: order.StockID, Valid: true},
	}); err != nil {
		return false, err
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
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
func cancelOrder(ctx context.Context, q *db.Queries, order db.ListAllOpenOrdersRow, userID int64, reason string) error {
	if err := q.CancelLimitOrder(ctx, order.ID); err != nil {
		return err
	}
	title := "Limit Order Cancelled"
	if order.Kind == "market" {
		title = "Queued Order Cancelled"
	}
	return q.CreateNotification(ctx, db.CreateNotificationParams{
		UserID:  userID,
		GameID:  nint64(order.GameID),
		Type:    "order_cancelled",
		Title:   title,
		Message: "Your " + order.Type + " order for " + itoa(order.Quantity) + " shares of " + order.Symbol + " was cancelled: " + reason + ".",
	})
}
