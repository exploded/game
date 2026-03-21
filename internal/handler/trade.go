package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) TradeForm(w http.ResponseWriter, r *http.Request) {
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

	latestPrice, _ := h.q.GetLatestPrice(r.Context(), stock.ID)
	holding, _ := h.q.GetHolding(r.Context(), db.GetHoldingParams{
		ParticipantID: participant.ID, StockID: stock.ID,
	})

	// Get exchange rate if currencies differ.
	var exchangeRate int64 = 1000000 // 1:1
	if stock.Currency != game.BaseCurrency {
		rate, err := h.q.GetLatestExchangeRate(r.Context())
		if err == nil {
			exchangeRate = rate.RateAudUsd
		}
	}

	h.render(w, r, "trade/form", "", PageData{
		Title: "Trade " + stock.Symbol,
		Item:  stock,
		Extra: map[string]any{
			"Game":         game,
			"Participant":  participant,
			"LatestPrice":  latestPrice,
			"Holding":      holding,
			"ExchangeRate": exchangeRate,
		},
	})
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

	latestPrice, err := h.q.GetLatestPrice(r.Context(), stock.ID)
	if err != nil || latestPrice.Close == 0 {
		setFlashCookie(w, "No price data available for this stock", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
		return
	}

	r.ParseForm()
	tradeType := r.FormValue("type") // "buy" or "sell"
	quantity, _ := strconv.ParseInt(r.FormValue("quantity"), 10, 64)

	if quantity <= 0 {
		setFlashCookie(w, "Quantity must be positive", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
		return
	}

	// Get exchange rate.
	var exchangeRate int64 = 1000000
	if stock.Currency != game.BaseCurrency {
		rate, err := h.q.GetLatestExchangeRate(r.Context())
		if err == nil {
			exchangeRate = rate.RateAudUsd
		}
	}

	pricePerShare := latestPrice.Close
	totalNative := quantity * pricePerShare
	totalBase := convertCurrency(totalNative, stock.Currency, game.BaseCurrency, exchangeRate)

	tx, err := h.rawDB.BeginTx(r.Context(), nil)
	if err != nil {
		setFlashCookie(w, "Internal error", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
		return
	}
	defer tx.Rollback()
	qtx := h.q.WithTx(tx)

	if tradeType == "buy" {
		cost := totalBase + game.TradeFee
		if cost > participant.CashBalance {
			setFlashCookie(w, "Insufficient funds", "error")
			http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
			return
		}

		// Deduct cash.
		err = qtx.UpdateCashBalance(r.Context(), db.UpdateCashBalanceParams{
			CashBalance: participant.CashBalance - cost,
			ID:          participant.ID,
		})
		if err != nil {
			setFlashCookie(w, "Failed to update balance", "error")
			http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
			return
		}

		// Upsert holding.
		existing, _ := qtx.GetHolding(r.Context(), db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: stock.ID,
		})
		newQty := existing.Quantity + quantity
		var newAvg int64
		if existing.ID != 0 {
			newAvg = ((existing.Quantity * existing.AvgCost) + (quantity * pricePerShare)) / newQty
		} else {
			newAvg = pricePerShare
		}
		err = qtx.UpsertHolding(r.Context(), db.UpsertHoldingParams{
			ParticipantID: participant.ID,
			StockID:       stock.ID,
			Quantity:      newQty,
			AvgCost:       newAvg,
		})
		if err != nil {
			setFlashCookie(w, "Failed to update holding", "error")
			http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
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
			http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
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
			http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
			return
		}

		// Update or delete holding. Supports negative qty for short positions.
		newQty := holding.Quantity - quantity
		if newQty == 0 {
			_ = qtx.DeleteHolding(r.Context(), db.DeleteHoldingParams{
				ParticipantID: participant.ID, StockID: stock.ID,
			})
		} else {
			avgCost := holding.AvgCost
			if newQty < 0 && holding.Quantity <= 0 {
				// Deepening short — track at current price.
				avgCost = pricePerShare
			} else if newQty < 0 && holding.Quantity > 0 {
				// Went from long to short.
				avgCost = pricePerShare
			}
			_ = qtx.UpsertHolding(r.Context(), db.UpsertHoldingParams{
				ParticipantID: participant.ID,
				StockID:       stock.ID,
				Quantity:      newQty,
				AvgCost:       avgCost,
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
		http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
		return
	}

	if err != nil {
		setFlashCookie(w, "Trade failed", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
		return
	}

	if err := tx.Commit(); err != nil {
		setFlashCookie(w, "Trade failed", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d/trade/%d", gameID, stockID), http.StatusSeeOther)
		return
	}

	action := "Bought"
	if tradeType == "sell" {
		action = "Sold"
	}

	// Record in activity feed.
	h.InsertTradeActivity(r, gameID, user.ID, tradeType, stock.Symbol, quantity, participant.IsPublic)

	// Check achievements.
	go h.CheckAndGrantAchievements(r.Context(), user.ID, gameID, participant.ID)

	setFlashCookie(w, fmt.Sprintf("%s %d shares of %s", action, quantity, stock.Symbol), "success")
	http.Redirect(w, r, fmt.Sprintf("/games/%d/portfolio", gameID), http.StatusSeeOther)
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
