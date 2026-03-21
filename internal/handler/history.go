package handler

import (
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/db"
)

// GameHistory shows finished/cancelled games (Feature 20).
func (h *Handler) GameHistory(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
	if page < 1 {
		page = 1
	}
	perPage := int64(20)
	offset := (page - 1) * perPage

	games, _ := h.q.ListFinishedGames(r.Context(), db.ListFinishedGamesParams{
		Limit: perPage, Offset: offset,
	})
	total, _ := h.q.CountFinishedGames(r.Context())

	h.render(w, r, "history/index", "", PageData{
		Title: "Game History",
		Items: games,
		Extra: map[string]any{
			"Page":       page,
			"TotalPages": (total + perPage - 1) / perPage,
		},
	})
}
