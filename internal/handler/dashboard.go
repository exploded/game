package handler

import (
	"fmt"
	"net/http"

	"github.com/exploded/game/internal/auth"
)

// Dashboard takes the player straight into this quarter's game, joining
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
		Items: h.quarterCards(r.Context(), user.ID),
	})
}
