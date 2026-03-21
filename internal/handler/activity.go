package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/db"
)

func (h *Handler) ActivityFeed(w http.ResponseWriter, r *http.Request) {
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

	page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
	if page < 1 {
		page = 1
	}
	perPage := int64(30)
	offset := (page - 1) * perPage

	activities, _ := h.q.ListActivityFeed(r.Context(), db.ListActivityFeedParams{
		GameID: gameID,
		Limit:  perPage,
		Offset: offset,
	})

	h.render(w, r, "activity/index", "", PageData{
		Title: "Activity - " + game.Name,
		Item:  game,
		Items: activities,
		Extra: map[string]any{
			"Page": page,
		},
	})
}

// InsertTradeActivity records a trade in the activity feed.
func (h *Handler) InsertTradeActivity(r *http.Request, gameID, userID int64, action, symbol string, quantity int64, isPublic int64) {
	verb := "bought"
	if action == "sell" {
		verb = "sold"
	}
	detail := fmt.Sprintf("%s %d shares of %s", verb, quantity, symbol)
	_ = h.q.InsertActivity(r.Context(), db.InsertActivityParams{
		GameID:   gameID,
		UserID:   userID,
		Action:   action,
		Detail:   detail,
		IsPublic: isPublic,
	})
}
