package handler

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// marketClosed reports whether a stock's market is closed. A variable so tests
// can open and close the market.
var marketClosed = market.MarketClosed

// queuesMarketOrder reports whether a market order in game g waits for the
// open. With after-hours trading, the default, it fills straight away at the
// last price instead.
func queuesMarketOrder(g db.Game, stockMarket string) bool {
	return g.AfterHoursTrading == 0 && marketClosed(stockMarket)
}

// TradeForm used to be a separate trade page. Trading now happens in the
// order ticket on the stock page, so old links land there.
func (h *Handler) TradeForm(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, fmt.Sprintf("/games/%s/stocks/%s", r.PathValue("id"), r.PathValue("sid")), http.StatusSeeOther)
}

// tradeContext is what the order ticket needs to know about the user's
// position in one stock.
type tradeContext struct {
	Participant  db.Participant
	Holding      db.Holding
	HoldingValue int64 // in the game's base currency
	ExchangeRate int64 // AUD/USD * 1,000,000; 1,000,000 when the currencies match
	Queued       bool  // the market is closed, so a market order queues for the open
	Closed       bool  // the market is closed, so a market order fills at the last price
}

// loadTradeContext returns the ticket data, or false if the user isn't in
// the game.
func (h *Handler) loadTradeContext(ctx context.Context, game db.Game, stock db.Stock, latest db.StockPrice, userID int64) (*tradeContext, bool) {
	participant, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{
		GameID: game.ID, UserID: userID,
	})
	if err != nil {
		return nil, false
	}
	tc := &tradeContext{Participant: participant, ExchangeRate: 1000000, Queued: queuesMarketOrder(game, stock.Market)}
	tc.Closed = !tc.Queued && marketClosed(stock.Market)
	tc.Holding, _ = h.q.GetHolding(ctx, db.GetHoldingParams{
		ParticipantID: participant.ID, StockID: stock.ID,
	})
	if stock.Currency != game.BaseCurrency {
		if rate, err := h.q.GetLatestExchangeRate(ctx); err == nil {
			tc.ExchangeRate = rate.RateAudUsd
		}
	}
	if tc.Holding.ID != 0 && latest.ID != 0 {
		tc.HoldingValue = convertCurrency(tc.Holding.Quantity*latest.Close, stock.Currency, game.BaseCurrency, tc.ExchangeRate)
	}
	return tc, true
}

func (h *Handler) TradeExecute(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	stockID, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(r.Context(), gameID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if game.Status != "active" {
		setFlashCookie(w, "Game is not active", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	stock, err := h.q.GetStock(r.Context(), stockID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	back := fmt.Sprintf("/games/%d/stocks/%d", gameID, stockID)
	r.ParseForm()
	if r.FormValue("from") == "home" {
		// The Close position button on the game's home page.
		back = fmt.Sprintf("/games/%d", gameID)
	}
	tradeType := r.FormValue("side") // "buy" or "sell"
	if tradeType == "" {
		tradeType = r.FormValue("type") // the old trade page's field
	}
	if r.FormValue("order_type") == "limit" {
		h.placeLimitOrder(w, r, game, participant, stock, tradeType)
		return
	}

	latestPrice, err := h.q.GetLatestPrice(r.Context(), stock.ID)
	if err != nil || latestPrice.Close == 0 {
		setFlashCookie(w, "No price data available for this stock", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	quantity, _ := strconv.ParseInt(r.FormValue("quantity"), 10, 64)

	// all=1 closes the whole position, however many shares that is now: a
	// sell for shares held, a buy for a short. A second tap then finds
	// nothing to close instead of going short or long.
	closeAll := r.FormValue("all") == "1"
	if closeAll {
		held, _ := h.q.GetHolding(r.Context(), db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: stock.ID,
		})
		switch {
		case tradeType == "sell" && held.Quantity > 0:
			quantity = held.Quantity
		case tradeType == "buy" && held.Quantity < 0:
			quantity = -held.Quantity
		case tradeType == "buy":
			setFlashCookie(w, "You aren't short "+stock.Symbol, "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		default:
			setFlashCookie(w, "You don't hold any "+stock.Symbol, "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
	}

	if quantity <= 0 {
		setFlashCookie(w, "Quantity must be positive", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	// Get exchange rate. Without one, converting 1:1 would misprice the trade.
	var exchangeRate int64 = 1000000
	if stock.Currency != game.BaseCurrency {
		rate, err := h.q.GetLatestExchangeRate(r.Context())
		if err != nil {
			setFlashCookie(w, "We don't have an exchange rate yet. Try again in a minute.", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		exchangeRate = rate.RateAudUsd
	}

	pricePerShare := latestPrice.Close

	// Guard against integer overflow (quantity * price).
	if pricePerShare != 0 && quantity > math.MaxInt64/pricePerShare {
		setFlashCookie(w, "Trade value too large", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	totalNative := quantity * pricePerShare
	totalBase := convertCurrency(totalNative, stock.Currency, game.BaseCurrency, exchangeRate)

	if queuesMarketOrder(game, stock.Market) {
		if closeAll && h.closeQueued(r.Context(), participant.ID, stock.ID, tradeType) {
			setFlashCookie(w, "You've already queued an order to close "+stock.Symbol, "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		h.queueMarketOrder(w, r, game, participant, stock, tradeType, quantity, totalBase, back)
		return
	}

	tx, err := h.rawDB.BeginTx(r.Context(), nil)
	if err != nil {
		setFlashCookie(w, "Internal error", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	defer tx.Rollback()
	qtx := h.q.WithTx(tx)

	if tradeType == "buy" {
		cost := totalBase + game.TradeFee
		if cost > participant.CashBalance {
			setFlashCookie(w, "Insufficient funds", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}

		// Deduct cash.
		err = qtx.UpdateCashBalance(r.Context(), db.UpdateCashBalanceParams{
			CashBalance: participant.CashBalance - cost,
			ID:          participant.ID,
		})
		if err != nil {
			setFlashCookie(w, "Failed to update balance", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}

		// Upsert holding.
		existing, _ := qtx.GetHolding(r.Context(), db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: stock.ID,
		})
		if closeAll && existing.Quantity != -quantity {
			// Another trade got in first.
			setFlashCookie(w, "Your "+stock.Symbol+" position just changed. Check it and try again.", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		newQty, newCost := market.Fill(existing.Quantity, existing.CostBasis, "buy", quantity, pricePerShare)
		if newQty == 0 {
			// Bought back a short exactly.
			err = qtx.DeleteHolding(r.Context(), db.DeleteHoldingParams{
				ParticipantID: participant.ID, StockID: stock.ID,
			})
		} else {
			err = qtx.UpsertHolding(r.Context(), db.UpsertHoldingParams{
				ParticipantID: participant.ID,
				StockID:       stock.ID,
				Quantity:      newQty,
				AvgCost:       market.AvgCost(newQty, newCost),
				CostBasis:     newCost,
				CurrentValue:  newQty * pricePerShare,
			})
		}
		if err != nil {
			setFlashCookie(w, "Failed to update holding", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}

		// Insert transaction.
		_, err = qtx.InsertTransaction(r.Context(), db.InsertTransactionParams{
			ParticipantID:    participant.ID,
			StockID:          stock.ID,
			Type:             "buy",
			Quantity:         quantity,
			Price:            pricePerShare,
			Total:            totalNative,
			ConvertedTotal:   totalBase,
			ExchangeRateUsed: exchangeRate,
			Fee:              game.TradeFee,
		})

	} else if tradeType == "sell" {
		holding, _ := qtx.GetHolding(r.Context(), db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: stock.ID,
		})
		if holding.Quantity < quantity && game.AllowShort == 0 {
			setFlashCookie(w, "Insufficient shares (short selling not allowed)", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
		if closeAll && holding.Quantity != quantity {
			// Another trade got in first.
			setFlashCookie(w, "Your "+stock.Symbol+" holding just changed. Check it and try again.", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}

		proceeds := totalBase - game.TradeFee
		if proceeds < 0 {
			proceeds = 0
		}

		// Add cash.
		err = qtx.UpdateCashBalance(r.Context(), db.UpdateCashBalanceParams{
			CashBalance: participant.CashBalance + proceeds,
			ID:          participant.ID,
		})
		if err != nil {
			setFlashCookie(w, "Failed to update balance", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}

		// Update or delete holding. Supports negative qty for short positions.
		newQty, newCost := market.Fill(holding.Quantity, holding.CostBasis, "sell", quantity, pricePerShare)
		if newQty == 0 {
			_ = qtx.DeleteHolding(r.Context(), db.DeleteHoldingParams{
				ParticipantID: participant.ID, StockID: stock.ID,
			})
		} else {
			_ = qtx.UpsertHolding(r.Context(), db.UpsertHoldingParams{
				ParticipantID: participant.ID,
				StockID:       stock.ID,
				Quantity:      newQty,
				AvgCost:       market.AvgCost(newQty, newCost),
				CostBasis:     newCost,
				CurrentValue:  newQty * pricePerShare,
			})
		}

		// Insert transaction.
		_, err = qtx.InsertTransaction(r.Context(), db.InsertTransactionParams{
			ParticipantID:    participant.ID,
			StockID:          stock.ID,
			Type:             "sell",
			Quantity:         quantity,
			Price:            pricePerShare,
			Total:            totalNative,
			ConvertedTotal:   totalBase,
			ExchangeRateUsed: exchangeRate,
			Fee:              game.TradeFee,
		})
	} else {
		setFlashCookie(w, "Invalid trade type", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	if err != nil {
		setFlashCookie(w, "Trade failed", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	if err := tx.Commit(); err != nil {
		setFlashCookie(w, "Trade failed", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	// Immediately revalue this participant's portfolio so the summary is correct.
	h.revalueParticipant(r.Context(), participant.ID, game.BaseCurrency)

	action := "Bought"
	if tradeType == "sell" {
		action = "Sold"
	}

	// Record in activity feed.
	h.InsertTradeActivity(r, gameID, user.ID, tradeType, stock.Symbol, quantity, participant.IsPublic)

	// Check achievements.
	go h.CheckAndGrantAchievements(context.Background(), user.ID, gameID, participant.ID)

	setFlashCookie(w, fmt.Sprintf("%s %s of %s", action, shares(quantity), stock.Symbol), "success")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// closeQueued reports whether the player has a market order of this side in
// this stock waiting for the market to open.
func (h *Handler) closeQueued(ctx context.Context, participantID, stockID int64, side string) bool {
	orders, _ := h.q.ListOpenLimitOrders(ctx, participantID)
	for _, o := range orders {
		if o.StockID == stockID && o.Type == side && o.Kind == "market" {
			return true
		}
	}
	return false
}

// revalueParticipant recalculates portfolio_value for a single participant
// after a trade. The trade's own rate won't do: it's 1:1 for a stock in the
// game's currency, and other holdings may not be. Without a rate the value
// waits for the scheduler's next revaluation.
func (h *Handler) revalueParticipant(ctx context.Context, participantID int64, baseCurrency string) {
	holdings, err := h.q.ListHoldings(ctx, participantID)
	if err != nil {
		return
	}
	var rate int64
	if er, err := h.q.GetLatestExchangeRate(ctx); err == nil {
		rate = er.RateAudUsd
	}
	holdingsValue, ok := market.HoldingsValue(holdings, baseCurrency, rate)
	if !ok {
		return
	}
	p, err := h.q.GetParticipant(ctx, participantID)
	if err != nil {
		return
	}
	h.q.UpdatePortfolioValue(ctx, db.UpdatePortfolioValueParams{
		PortfolioValue: p.CashBalance + holdingsValue,
		ID:             participantID,
	})
}

// convertCurrency converts cents from one currency to another using the exchange rate.
// Rate is AUD/USD * 1,000,000 (e.g. 0.65 = 650000).
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

// Ensure the Queries type has WithTx method.
func init() {
	// This is a compile-time check that db.Queries has WithTx.
	var _ = (*db.Queries)(nil).WithTx
}

// Suppress unused import warning.
var _ = sql.ErrNoRows
