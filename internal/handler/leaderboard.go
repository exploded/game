package handler

import (
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) Leaderboard(w http.ResponseWriter, r *http.Request) {
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

	participants, _ := h.q.ListPublicParticipantsByGame(r.Context(), game.ID)

	// Check if current user is in the game.
	myParticipant, _ := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})

	h.render(w, r, "leaderboard/index", "", PageData{
		Title: "Leaderboard - " + game.Name,
		Item:  game,
		Items: participants,
		Extra: map[string]any{
			"MyParticipant": myParticipant,
		},
	})
}
