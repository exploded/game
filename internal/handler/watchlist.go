package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) WatchlistPage(w http.ResponseWriter, r *http.Request) {
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

	items, _ := h.q.ListWatchlist(r.Context(), participant.ID)

	// Get latest prices for each watchlist item.
	type WatchlistItem struct {
		db.ListWatchlistRow
		LatestPrice int64
		PriceDate   string
	}
	var enriched []WatchlistItem
	for _, item := range items {
		wi := WatchlistItem{ListWatchlistRow: item}
		if p, err := h.q.GetLatestPrice(r.Context(), item.StockID); err == nil {
			wi.LatestPrice = p.Close
			wi.PriceDate = p.Date
		}
		enriched = append(enriched, wi)
	}

	h.render(w, r, "watchlist/index", "", PageData{
		Title: "Watchlist - " + game.Name,
		Item:  game,
		Items: enriched,
		Extra: map[string]any{
			"Participant": participant,
		},
	})
}

func (h *Handler) WatchlistAdd(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	stockID, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	_ = h.q.AddToWatchlist(r.Context(), db.AddToWatchlistParams{
		ParticipantID: participant.ID,
		StockID:       stockID,
	})
	// Market Watcher: 10 stocks on one game's watchlist.
	if n, _ := h.q.CountWatchlist(r.Context(), participant.ID); n >= 10 {
		h.grantIfNew(r.Context(), user.ID, "watchlist_10", gameID)
	}

	if isHTMX(r) {
		h.renderWatch(w, r, gameID, stockID, true)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/games/%d/stocks/%d", gameID, stockID), http.StatusSeeOther)
}

func (h *Handler) WatchlistRemove(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	stockID, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	_ = h.q.RemoveFromWatchlist(r.Context(), db.RemoveFromWatchlistParams{
		ParticipantID: participant.ID,
		StockID:       stockID,
	})

	if isHTMX(r) {
		h.renderWatch(w, r, gameID, stockID, false)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/games/%d/watchlist", gameID), http.StatusSeeOther)
}

// renderWatch sends back the watch button that was clicked
// (partials/_watch.html): the stocks page's star for ?style=star, otherwise
// the stock page's Watch button.
func (h *Handler) renderWatch(w http.ResponseWriter, r *http.Request, gameID, stockID int64, watching bool) {
	frag := "watch-button-reply"
	if r.URL.Query().Get("style") == "star" {
		frag = "watch-star-reply"
	}
	var symbol string
	if s, err := h.q.GetStock(r.Context(), stockID); err == nil {
		symbol = s.Symbol
	}
	h.render(w, r, "stocks/browse", frag, PageData{Extra: map[string]any{
		"Game": gameID, "Stock": stockID, "Symbol": symbol, "Watching": watching,
	}})
}
