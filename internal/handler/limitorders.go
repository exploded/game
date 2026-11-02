package handler

import (
	"errors"
	"fmt"
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

// LimitOrderCreate places a limit order from the old orders form. The order
// ticket's Limit option goes through TradeExecute.
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
	r.ParseForm()
	o := Order{
		UserID:     user.ID,
		GameID:     gameID,
		StockID:    stockID,
		Side:       r.FormValue("type"),
		Limit:      true,
		LimitPrice: parseDollars(r.FormValue("limit_price")),
		ExpiresAt:  r.FormValue("expires_at"),
	}
	o.Quantity, _ = strconv.ParseInt(r.FormValue("quantity"), 10, 64)
	h.placeFromForm(w, r, o, fmt.Sprintf("/games/%d/stocks/%d", gameID, stockID), false)
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

	orders := fmt.Sprintf("/games/%d/orders", gameID)
	_, err = h.CancelOrder(r.Context(), user.ID, orderID)
	var refused *OrderError
	switch {
	case errors.Is(err, errNotFound):
		http.NotFound(w, r)
		return
	case errors.As(err, &refused):
		tradeError(w, r, refused.Msg, orders)
		return
	case err != nil:
		h.serverError(w, "cancel order", err)
		return
	}

	if isHTMX(r) {
		triggerToast(w, "Order cancelled", "success")
		w.Header().Set("HX-Redirect", orders)
		w.WriteHeader(http.StatusOK)
		return
	}
	setFlashCookie(w, "Order cancelled", "success")
	http.Redirect(w, r, orders, http.StatusSeeOther)
}
