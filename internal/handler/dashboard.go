package handler

import (
	"net/http"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	myGames, _ := h.q.ListUserGames(r.Context(), user.ID)
	activeGames, _ := h.q.ListActiveGames(r.Context())

	// The game you last opened, if it's still running.
	var current *db.Game
	if user.LastGameID.Valid {
		for _, g := range myGames {
			if g.ID == user.LastGameID.Int64 && (g.Status == "active" || g.Status == "pending") {
				current = &g
				break
			}
		}
	}

	h.render(w, r, "dashboard/index", "", PageData{
		Title: "Dashboard",
		Items: activeGames,
		Extra: map[string]any{
			"MyGames":     myGames,
			"CurrentGame": current,
		},
	})
}
