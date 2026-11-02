package handler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// Order is one order from a player: from the order ticket, the API or MCP.
type Order struct {
	UserID     int64
	GameID     int64
	StockID    int64
	Side       string // buy | sell
	Limit      bool   // a limit order; otherwise a market order
	Quantity   int64
	CloseAll   bool   // close the whole position, whatever Quantity says
	LimitPrice int64  // cents in the stock's currency, limit orders only
	ExpiresAt  string // YYYY-MM-DD, optional, limit orders only
	Source     string // "" from the site, "api" or "mcp" from the player's AI
	DryRun     bool   // work the order out, check it, and place nothing
}

// What happened to an order, in OrderResult.Status.
const (
	orderFilled = "filled" // a market order, filled straight away
	orderQueued = "queued" // a market order waiting for the market to open
	orderOpen   = "open"   // a limit order waiting for its price
)

// OrderResult is an order placed or, with DryRun, the order that would be.
type OrderResult struct {
	Status   string
	Message  string // for the player: "Bought 5 shares of ACME"
	Stock    db.Stock
	Side     string
	Quantity int64
	// Price is per share in the stock's currency: the fill, the last close a
	// queued order is estimated at, or a limit order's limit.
	Price         int64
	Value         int64  // Quantity * Price in the game's currency
	Fee           int64  // in the game's currency
	Currency      string // the game's currency
	CashAfter     int64  // cash once it fills at Price (now, for a filled order)
	ExpiresAt     string
	TransactionID int64
	OrderID       int64
}

// OrderError refuses an order, with a reason to show the player.
type OrderError struct{ Msg string }

func (e *OrderError) Error() string { return e.Msg }

func refuse(format string, a ...any) error { return &OrderError{fmt.Sprintf(format, a...)} }

// errNotFound is a game, stock or order that doesn't exist or isn't the
// player's.
var errNotFound = errors.New("not found")

// PlaceOrder checks an order and places it: a market order fills now or, with
// the market closed and after-hours trading off, queues for the open; a limit
// order waits for ProcessLimitOrders. A refusal is an *OrderError.
func (h *Handler) PlaceOrder(ctx context.Context, o Order) (OrderResult, error) {
	game, err := h.q.GetGame(ctx, o.GameID)
	if err != nil {
		return OrderResult{}, errNotFound
	}
	if game.Status != "active" {
		return OrderResult{}, refuse("Game is not active")
	}
	if o.Source != "" && game.AiTrading == 0 {
		return OrderResult{}, refuse("AI trading is turned off in this game. Trade on thewealthlab.au instead.")
	}
	stock, err := h.q.GetStock(ctx, o.StockID)
	if err != nil {
		return OrderResult{}, errNotFound
	}
	participant, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{
		GameID: game.ID, UserID: o.UserID,
	})
	if err != nil {
		return OrderResult{}, refuse("You're not playing in this game")
	}
	if o.Side != "buy" && o.Side != "sell" {
		return OrderResult{}, refuse("Invalid trade type")
	}
	if o.Limit {
		return h.placeLimit(ctx, o, game, stock, participant)
	}

	latestPrice, err := h.q.GetLatestPrice(ctx, stock.ID)
	if err != nil || latestPrice.Close == 0 {
		return OrderResult{}, refuse("No price data available for this stock")
	}

	quantity := o.Quantity
	// CloseAll closes the whole position, however many shares that is now: a
	// sell for shares held, a buy for a short. A second tap then finds
	// nothing to close instead of going short or long.
	if o.CloseAll {
		held, _ := h.q.GetHolding(ctx, db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: stock.ID,
		})
		switch {
		case o.Side == "sell" && held.Quantity > 0:
			quantity = held.Quantity
		case o.Side == "buy" && held.Quantity < 0:
			quantity = -held.Quantity
		case o.Side == "buy":
			return OrderResult{}, refuse("You aren't short %s", stock.Symbol)
		default:
			return OrderResult{}, refuse("You don't hold any %s", stock.Symbol)
		}
	}
	if quantity <= 0 {
		return OrderResult{}, refuse("Quantity must be positive")
	}

	// Without an exchange rate, converting 1:1 would misprice the trade.
	exchangeRate, err := h.exchangeRate(ctx, game, stock)
	if err != nil {
		return OrderResult{}, err
	}

	pricePerShare := latestPrice.Close
	if quantity > math.MaxInt64/pricePerShare {
		return OrderResult{}, refuse("Trade value too large")
	}
	totalNative := quantity * pricePerShare
	totalBase := convertCurrency(totalNative, stock.Currency, game.BaseCurrency, exchangeRate)
	fee := market.TradeFees(game, stock.Currency, totalBase)
	res := OrderResult{
		Status: orderFilled, Stock: stock, Side: o.Side, Quantity: quantity,
		Price: pricePerShare, Value: totalBase, Fee: fee, Currency: game.BaseCurrency,
	}

	if queuesMarketOrder(game, stock.Market) {
		if o.CloseAll && h.closeQueued(ctx, participant.ID, stock.ID, o.Side) {
			return OrderResult{}, refuse("You've already queued an order to close %s", stock.Symbol)
		}
		return h.queueMarket(ctx, o, game, stock, participant, res)
	}

	tx, err := h.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return OrderResult{}, err
	}
	defer tx.Rollback()
	qtx := h.q.WithTx(tx)

	// Read again inside the transaction: cash is written back whole, so a
	// balance read before it could undo another order placed in between.
	participant, err = qtx.GetParticipant(ctx, participant.ID)
	if err != nil {
		return OrderResult{}, err
	}
	holding, _ := qtx.GetHolding(ctx, db.GetHoldingParams{
		ParticipantID: participant.ID, StockID: stock.ID,
	})

	var cash int64
	if o.Side == "buy" {
		cash = participant.CashBalance - (totalBase + fee)
		if cash < 0 {
			return OrderResult{}, refuse("Insufficient funds")
		}
		if o.CloseAll && holding.Quantity != -quantity {
			// Another trade got in first.
			return OrderResult{}, refuse("Your %s position just changed. Check it and try again.", stock.Symbol)
		}
	} else {
		if holding.Quantity < quantity && game.AllowShort == 0 {
			return OrderResult{}, refuse("Insufficient shares (short selling not allowed)")
		}
		if o.CloseAll && holding.Quantity != quantity {
			// Another trade got in first.
			return OrderResult{}, refuse("Your %s holding just changed. Check it and try again.", stock.Symbol)
		}
		cash = participant.CashBalance + max(totalBase-fee, 0)
	}
	res.CashAfter = cash
	if o.DryRun {
		return res, nil
	}

	if err := qtx.UpdateCashBalance(ctx, db.UpdateCashBalanceParams{CashBalance: cash, ID: participant.ID}); err != nil {
		return OrderResult{}, err
	}
	// Supports negative quantities for short positions.
	newQty, newCost := market.Fill(holding.Quantity, holding.CostBasis, o.Side, quantity, pricePerShare)
	if newQty == 0 {
		err = qtx.DeleteHolding(ctx, db.DeleteHoldingParams{ParticipantID: participant.ID, StockID: stock.ID})
	} else {
		err = qtx.UpsertHolding(ctx, db.UpsertHoldingParams{
			ParticipantID: participant.ID,
			StockID:       stock.ID,
			Quantity:      newQty,
			AvgCost:       market.AvgCost(newQty, newCost),
			CostBasis:     newCost,
			CurrentValue:  newQty * pricePerShare,
		})
	}
	if err != nil {
		return OrderResult{}, err
	}
	t, err := qtx.InsertTransaction(ctx, db.InsertTransactionParams{
		ParticipantID:    participant.ID,
		StockID:          stock.ID,
		Type:             o.Side,
		Quantity:         quantity,
		Price:            pricePerShare,
		Total:            totalNative,
		ConvertedTotal:   totalBase,
		ExchangeRateUsed: exchangeRate,
		Fee:              fee,
		Source:           o.Source,
	})
	if err != nil {
		return OrderResult{}, err
	}
	if err := tx.Commit(); err != nil {
		return OrderResult{}, err
	}
	res.TransactionID = t.ID

	// Revalue now so the portfolio summary is right straight away.
	h.revalueParticipant(ctx, participant.ID, game.BaseCurrency)
	h.InsertTradeActivity(ctx, game.ID, o.UserID, o.Side, stock, quantity)
	go h.CheckAndGrantAchievements(context.Background(), o.UserID, game.ID, participant.ID)

	action := "Bought"
	if o.Side == "sell" {
		action = "Sold"
	}
	res.Message = fmt.Sprintf("%s %s of %s", action, shares(quantity), stock.Symbol)
	return res, nil
}

// exchangeRate is the AUD/USD rate for trading stock in game, or 1,000,000
// when the currencies match. With no rate stored it refuses the order.
func (h *Handler) exchangeRate(ctx context.Context, game db.Game, stock db.Stock) (int64, error) {
	if stock.Currency == game.BaseCurrency {
		return 1000000, nil
	}
	rate, err := h.q.GetLatestExchangeRate(ctx)
	if err != nil {
		return 0, refuse("We don't have an exchange rate yet. Try again in a minute.")
	}
	return rate.RateAudUsd, nil
}

// queueMarket stores a market order placed while the market is closed. The
// scheduler fills it at the first price after the open. res holds the
// estimate at the last close; cash is checked again at the fill, when the
// order is cancelled if it's short.
func (h *Handler) queueMarket(ctx context.Context, o Order, game db.Game, stock db.Stock, participant db.Participant, res OrderResult) (OrderResult, error) {
	res.Status = orderQueued
	if o.Side == "buy" {
		res.CashAfter = participant.CashBalance - res.Value - res.Fee
		if res.CashAfter < 0 {
			return OrderResult{}, refuse("Insufficient funds")
		}
	} else {
		holding, _ := h.q.GetHolding(ctx, db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: stock.ID,
		})
		if holding.Quantity < res.Quantity && game.AllowShort == 0 {
			return OrderResult{}, refuse("Insufficient shares (short selling not allowed)")
		}
		res.CashAfter = participant.CashBalance + max(res.Value-res.Fee, 0)
	}
	if o.DryRun {
		return res, nil
	}

	lo, err := h.q.CreateMarketOrder(ctx, db.CreateMarketOrderParams{
		ParticipantID: participant.ID,
		StockID:       stock.ID,
		Type:          o.Side,
		Quantity:      res.Quantity,
		Source:        o.Source,
	})
	if err != nil {
		return OrderResult{}, err
	}
	res.OrderID = lo.ID
	opens := ""
	if when := marketOpens(marketNow()); when != "" {
		opens = " " + when
	}
	res.Message = fmt.Sprintf("Order queued: %s %s of %s. The US market is closed, so it fills when it opens%s.", o.Side, shares(res.Quantity), stock.Symbol, opens)
	return res, nil
}

// placeLimit checks and stores a limit order.
func (h *Handler) placeLimit(ctx context.Context, o Order, game db.Game, stock db.Stock, participant db.Participant) (OrderResult, error) {
	if game.LimitOrders == 0 {
		return OrderResult{}, refuse("Limit orders are turned off in this game")
	}
	if o.Quantity <= 0 || o.LimitPrice <= 0 {
		return OrderResult{}, refuse("Quantity and price must be positive")
	}
	if o.Quantity > math.MaxInt64/o.LimitPrice {
		return OrderResult{}, refuse("Trade value too large")
	}
	var expiry sql.NullString
	if o.ExpiresAt != "" {
		day, err := time.ParseInLocation(time.DateOnly, o.ExpiresAt, h.loc)
		if err != nil {
			return OrderResult{}, refuse("Give the expiry date as YYYY-MM-DD")
		}
		if day.Format(time.DateOnly) < time.Now().In(h.loc).Format(time.DateOnly) {
			return OrderResult{}, refuse("The expiry date has passed")
		}
		expiry = sql.NullString{String: o.ExpiresAt, Valid: true}
	}

	exchangeRate, err := h.exchangeRate(ctx, game, stock)
	if err != nil {
		return OrderResult{}, err
	}
	value := convertCurrency(o.Quantity*o.LimitPrice, stock.Currency, game.BaseCurrency, exchangeRate)
	fee := market.TradeFees(game, stock.Currency, value)
	res := OrderResult{
		Status: orderOpen, Stock: stock, Side: o.Side, Quantity: o.Quantity,
		Price: o.LimitPrice, Value: value, Fee: fee, Currency: game.BaseCurrency,
		ExpiresAt: o.ExpiresAt,
	}
	// Limit sells can't go short: ProcessLimitOrders cancels one without the
	// shares when it fills.
	if o.Side == "sell" {
		holding, _ := h.q.GetHolding(ctx, db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: stock.ID,
		})
		if holding.Quantity < o.Quantity {
			return OrderResult{}, refuse("Insufficient shares for limit sell order")
		}
		res.CashAfter = participant.CashBalance + max(value-fee, 0)
	} else {
		res.CashAfter = participant.CashBalance - value - fee
		if res.CashAfter < 0 {
			return OrderResult{}, refuse("Insufficient funds")
		}
	}
	if o.DryRun {
		return res, nil
	}

	lo, err := h.q.CreateLimitOrder(ctx, db.CreateLimitOrderParams{
		ParticipantID: participant.ID,
		StockID:       stock.ID,
		Type:          o.Side,
		Quantity:      o.Quantity,
		LimitPrice:    o.LimitPrice,
		ExpiresAt:     expiry,
		Source:        o.Source,
	})
	if err != nil {
		return OrderResult{}, err
	}
	res.OrderID = lo.ID
	res.Message = fmt.Sprintf("Limit %s order placed for %s of %s at %s", o.Side, shares(o.Quantity), stock.Symbol, fmtCents(o.LimitPrice))
	return res, nil
}

// CancelOrder cancels one of the player's open orders. It's errNotFound if
// the order isn't theirs.
func (h *Handler) CancelOrder(ctx context.Context, userID, orderID int64) (db.LimitOrder, error) {
	order, err := h.q.GetLimitOrder(ctx, orderID)
	if err != nil {
		return db.LimitOrder{}, errNotFound
	}
	p, err := h.q.GetParticipant(ctx, order.ParticipantID)
	if err != nil || p.UserID != userID {
		return db.LimitOrder{}, errNotFound
	}
	n, err := h.q.CancelOpenOrder(ctx, order.ID)
	if err != nil {
		return db.LimitOrder{}, err
	}
	if n == 0 {
		return db.LimitOrder{}, refuse("That order isn't open any more: it filled, expired or was cancelled")
	}
	return order, nil
}
