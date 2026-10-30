package handler

import (
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) LimitOrdersList(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(r.Context(), gameID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		setFlashCookie(w, "Join the game first", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	orders, _ := h.q.ListOpenLimitOrders(r.Context(), participant.ID)

	h.render(w, r, "orders/list", "", PageData{
		Title: "Orders - " + game.Name,
		Item:  game,
		Items: orders,
		Extra: map[string]any{
			"Participant": participant,
		},
	})
}

func (h *Handler) LimitOrderCreate(w http.ResponseWriter, r *http.Request) {
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

	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	stock, err := h.q.GetStock(r.Context(), stockID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	r.ParseForm()
	h.placeLimitOrder(w, r, game, participant, stock, r.FormValue("type"))
}

// placeLimitOrder validates and stores a limit order from the form in r. Both
// LimitOrderCreate and the order ticket's Limit option (via TradeExecute) use it.
func (h *Handler) placeLimitOrder(w http.ResponseWriter, r *http.Request, game db.Game, participant db.Participant, stock db.Stock, orderType string) {
	back := fmt.Sprintf("/games/%d/stocks/%d", game.ID, stock.ID)
	quantity, _ := strconv.ParseInt(r.FormValue("quantity"), 10, 64)
	limitPriceF, _ := strconv.ParseFloat(r.FormValue("limit_price"), 64)
	limitPrice := int64(math.Round(limitPriceF * 100))
	expiresAt := r.FormValue("expires_at")

	if quantity <= 0 || limitPrice <= 0 {
		setFlashCookie(w, "Quantity and price must be positive", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	if orderType != "buy" && orderType != "sell" {
		setFlashCookie(w, "Invalid order type", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	// Validate sell: must have enough shares.
	if orderType == "sell" {
		holding, _ := h.q.GetHolding(r.Context(), db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: stock.ID,
		})
		if holding.Quantity < quantity {
			setFlashCookie(w, "Insufficient shares for limit sell order", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
	}

	var expiry sql.NullString
	if expiresAt != "" {
		expiry = sql.NullString{String: expiresAt, Valid: true}
	}

	_, err := h.q.CreateLimitOrder(r.Context(), db.CreateLimitOrderParams{
		ParticipantID: participant.ID,
		StockID:       stock.ID,
		Type:          orderType,
		Quantity:      quantity,
		LimitPrice:    limitPrice,
		ExpiresAt:     expiry,
	})
	if err != nil {
		setFlashCookie(w, "Failed to create order", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	setFlashCookie(w, fmt.Sprintf("Limit %s order placed for %s of %s at %s", orderType, shares(quantity), stock.Symbol, fmtCents(limitPrice)), "success")
	http.Redirect(w, r, fmt.Sprintf("/games/%d/orders", game.ID), http.StatusSeeOther)
}

// queueMarketOrder stores a market order placed while the market is closed.
// The scheduler fills it at the first price after the open. estimate is the
// trade's value at the last price, in the game's base currency; cash is
// checked again at the fill, when the order is cancelled if it's short.
// back is the page the order came from, for errors.
func (h *Handler) queueMarketOrder(w http.ResponseWriter, r *http.Request, game db.Game, participant db.Participant, stock db.Stock, side string, quantity, estimate int64, back string) {
	switch side {
	case "buy":
		if estimate+game.TradeFee > participant.CashBalance {
			setFlashCookie(w, "Insufficient funds", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
	case "sell":
		holding, _ := h.q.GetHolding(r.Context(), db.GetHoldingParams{
			ParticipantID: participant.ID, StockID: stock.ID,
		})
		if holding.Quantity < quantity && game.AllowShort == 0 {
			setFlashCookie(w, "Insufficient shares (short selling not allowed)", "error")
			http.Redirect(w, r, back, http.StatusSeeOther)
			return
		}
	default:
		setFlashCookie(w, "Invalid trade type", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	if _, err := h.q.CreateMarketOrder(r.Context(), db.CreateMarketOrderParams{
		ParticipantID: participant.ID,
		StockID:       stock.ID,
		Type:          side,
		Quantity:      quantity,
	}); err != nil {
		setFlashCookie(w, "Failed to create order", "error")
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}

	setFlashCookie(w, fmt.Sprintf("Order queued: %s %s of %s. The US market is closed, so it fills when it opens at 9:30 am New York time.", side, shares(quantity), stock.Symbol), "success")
	done := fmt.Sprintf("/games/%d/orders", game.ID)
	if back == fmt.Sprintf("/games/%d", game.ID) {
		done = back // queued from the home page's Sell button: stay there
	}
	http.Redirect(w, r, done, http.StatusSeeOther)
}

func (h *Handler) LimitOrderCancel(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	orderID, err := strconv.ParseInt(r.PathValue("oid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	order, err := h.q.GetLimitOrder(r.Context(), orderID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Verify ownership.
	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil || participant.ID != order.ParticipantID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	_ = h.q.CancelLimitOrder(r.Context(), order.ID)

	if isHTMX(r) {
		triggerToast(w, "Order cancelled", "success")
		w.Header().Set("HX-Redirect", fmt.Sprintf("/games/%d/orders", gameID))
		w.WriteHeader(http.StatusOK)
		return
	}
	setFlashCookie(w, "Order cancelled", "success")
	http.Redirect(w, r, fmt.Sprintf("/games/%d/orders", gameID), http.StatusSeeOther)
}
