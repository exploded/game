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

	if isHTMX(r) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf(`<button hx-delete="/games/%d/watchlist/%d" hx-swap="outerHTML" hx-disabled-elt="this" class="btn btn-sm btn-watchlist-active">Watching</button>`, gameID, stockID)))
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
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf(`<button hx-post="/games/%d/watchlist/%d" hx-swap="outerHTML" hx-disabled-elt="this" class="btn btn-sm">Watch</button>`, gameID, stockID)))
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/games/%d/watchlist", gameID), http.StatusSeeOther)
}
