package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
)

// Dashboard takes the player straight into this month's game, joining
// them if they aren't in it yet. Until the scheduler has created the game it
// shows the games there are.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if g, ok := h.currentGame(r.Context(), user.ID); ok {
		http.Redirect(w, r, fmt.Sprintf("/games/%d", g.ID), http.StatusSeeOther)
		return
	}
	h.render(w, r, "dashboard/index", "", PageData{
		Title: "Dashboard",
		Items: h.monthCards(r.Context(), user.ID),
	})
}

// StockInCurrentGame opens a stock's page in this month's game, joining the
// player as Dashboard does. Learn links stocks here, since it has no game.
func (h *Handler) StockInCurrentGame(w http.ResponseWriter, r *http.Request) {
	sid, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user := auth.UserFromContext(r.Context())
	g, ok := h.currentGame(r.Context(), user.ID)
	if !ok {
		http.Redirect(w, r, "/games", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/games/%d/stocks/%d", g.ID, sid), http.StatusSeeOther)
}
