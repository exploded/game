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

// LimitOrderCancel cancels an open order from the orders page (a plain form
// post). Limit orders are placed through the order ticket (TradeExecute).
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

	setFlashCookie(w, "Order cancelled", "success")
	http.Redirect(w, r, orders, http.StatusSeeOther)
}
