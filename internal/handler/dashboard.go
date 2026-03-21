package handler

import (
	"net/http"

	"github.com/exploded/game/internal/auth"
)

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	myGames, _ := h.q.ListUserGames(r.Context(), user.ID)
	activeGames, _ := h.q.ListActiveGames(r.Context())

	h.render(w, r, "dashboard/index", "", PageData{
		Title: "Dashboard",
		Items: activeGames,
		Extra: map[string]any{
			"MyGames": myGames,
		},
	})
}
