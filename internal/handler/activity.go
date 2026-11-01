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

	// One more than a page, to know whether there's an older page.
	activities, _ := h.q.ListActivityFeed(r.Context(), db.ListActivityFeedParams{
		GameID: gameID,
		Limit:  perPage + 1,
		Offset: offset,
	})
	more := int64(len(activities)) > perPage
	if more {
		activities = activities[:perPage]
	}

	h.render(w, r, "activity/index", "", PageData{
		Title: "Game activity - " + game.Name,
		Item:  game,
		Items: activities,
		Extra: map[string]any{
			"Page": page,
			"More": more,
		},
	})
}

// InsertTradeActivity records a trade in the activity feed.
func (h *Handler) InsertTradeActivity(r *http.Request, gameID, userID int64, action string, stock db.Stock, quantity int64) {
	verb := "bought"
	if action == "sell" {
		verb = "sold"
	}
	detail := fmt.Sprintf("%s %s of %s", verb, shares(quantity), stock.Symbol)
	_ = h.q.InsertActivity(r.Context(), db.InsertActivityParams{
		GameID:  gameID,
		UserID:  userID,
		Action:  action,
		Detail:  detail,
		StockID: nint64(stock.ID),
	})
}
