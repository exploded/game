package handler

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/game/internal/db"
)

// resolveStockBySearch finds a single stock matching the search term (ticker or name).
// Returns the stock and true, or zero-value and false with a flash error set.
func (h *Handler) resolveStockBySearch(w http.ResponseWriter, r *http.Request) (db.Stock, bool) {
	search := strings.TrimSpace(r.FormValue("stock_search"))
	if search == "" {
		setFlashCookie(w, "Please enter a stock ticker or name", "error")
		return db.Stock{}, false
	}
	pattern := "%" + search + "%"
	results, err := h.q.SearchStocks(r.Context(), db.SearchStocksParams{
		Market:  "both",
		Column2: "both",
		Symbol:  pattern,
		Name:    pattern,
	})
	if err != nil || len(results) == 0 {
		setFlashCookie(w, fmt.Sprintf("No stock found matching %q", search), "error")
		return db.Stock{}, false
	}
	// Prefer exact symbol match (case-insensitive).
	for _, s := range results {
		if strings.EqualFold(s.Symbol, search) {
			return s, true
		}
	}
	if len(results) > 1 {
		names := make([]string, len(results))
		for i, s := range results {
			names[i] = s.Symbol
		}
		setFlashCookie(w, fmt.Sprintf("Multiple matches: %s — be more specific", strings.Join(names, ", ")), "error")
		return db.Stock{}, false
	}
	return results[0], true
}

// AdminDividend simulates a dividend payment for a stock (Feature 4).
// All participants holding the stock in active games get cash credited.
func (h *Handler) AdminDividend(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	stock, ok := h.resolveStockBySearch(w, r)
	if !ok {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	stockID := stock.ID
	dividendPerShare, _ := strconv.ParseFloat(r.FormValue("dividend"), 64)
	if dividendPerShare <= 0 {
		setFlashCookie(w, "Dividend must be positive", "error")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	divCents := int64(math.Round(dividendPerShare * 100))

	// Find all active holdings for this stock.
	holdings, _ := h.q.ListAllActiveHoldings(r.Context())
	paid := 0
	for _, h_ := range holdings {
		if h_.StockID != stockID || h_.Quantity <= 0 {
			continue
		}
		dividendNative := h_.Quantity * divCents

		// Get the game to determine currency conversion.
		game, err := h.q.GetGame(r.Context(), h_.GameID)
		if err != nil {
			continue
		}

		var exchangeRate int64 = 1000000
		if stock.Currency != game.BaseCurrency {
			rate, err := h.q.GetLatestExchangeRate(r.Context())
			if err == nil {
				exchangeRate = rate.RateAudUsd
			}
		}

		dividendBase := convertCurrency(dividendNative, stock.Currency, game.BaseCurrency, exchangeRate)

		participant, err := h.q.GetParticipant(r.Context(), h_.ParticipantID)
		if err != nil {
			continue
		}

		_ = h.q.UpdateCashBalance(r.Context(), db.UpdateCashBalanceParams{
			CashBalance: participant.CashBalance + dividendBase,
			ID:          participant.ID,
		})

		// Notify.
		_ = h.q.CreateNotification(r.Context(), db.CreateNotificationParams{
			UserID:  participant.UserID,
			GameID:  nint64(h_.GameID),
			Type:    "dividend",
			Title:   "Dividend Received",
			Message: fmt.Sprintf("%s paid %s per share dividend (%d shares = %s)", stock.Symbol, fmtCents(divCents), h_.Quantity, fmtCents(dividendBase)),
		})
		paid++
	}

	setFlashCookie(w, fmt.Sprintf("Dividend of %s per share paid to %d holdings of %s", fmtCents(divCents), paid, stock.Symbol), "success")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// AdminStockSplit simulates a stock split (Feature 5).
// All holdings of this stock are multiplied by the ratio.
func (h *Handler) AdminStockSplit(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	stock, ok := h.resolveStockBySearch(w, r)
	if !ok {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	stockID := stock.ID
	ratioNum, _ := strconv.ParseInt(r.FormValue("ratio_num"), 10, 64)   // e.g. 2 for 2:1
	ratioDen, _ := strconv.ParseInt(r.FormValue("ratio_den"), 10, 64)   // e.g. 1 for 2:1
	if ratioNum <= 0 || ratioDen <= 0 {
		setFlashCookie(w, "Split ratio must be positive (e.g. 2:1)", "error")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}

	holdings, _ := h.q.ListAllActiveHoldings(r.Context())
	updated := 0
	for _, h_ := range holdings {
		if h_.StockID != stockID || h_.Quantity == 0 {
			continue
		}

		newQty := (h_.Quantity * ratioNum) / ratioDen
		newAvg := (h_.AvgCost * ratioDen) / ratioNum

		_ = h.q.UpsertHolding(r.Context(), db.UpsertHoldingParams{
			ParticipantID: h_.ParticipantID,
			StockID:       h_.StockID,
			Quantity:      newQty,
			AvgCost:       newAvg,
			CurrentValue:  h_.CurrentValue, // preserve existing; revaluation will update
		})

		participant, _ := h.q.GetParticipant(r.Context(), h_.ParticipantID)
		_ = h.q.CreateNotification(r.Context(), db.CreateNotificationParams{
			UserID:  participant.UserID,
			GameID:  nint64(h_.GameID),
			Type:    "stock_split",
			Title:   "Stock Split",
			Message: fmt.Sprintf("%s split %d:%d - your holdings adjusted from %d to %d shares", stock.Symbol, ratioNum, ratioDen, h_.Quantity, newQty),
		})
		updated++
	}

	setFlashCookie(w, fmt.Sprintf("%s %d:%d split applied to %d holdings", stock.Symbol, ratioNum, ratioDen, updated), "success")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
