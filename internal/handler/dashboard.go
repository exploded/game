package handler

import (
	"net/http"

	"github.com/exploded/game/internal/auth"
)

// Dashboard shows this quarter's game and the next one, to open or join.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	h.render(w, r, "dashboard/index", "", PageData{
		Title: "Dashboard",
		Items: h.quarterCards(r.Context(), user.ID),
	})
}
