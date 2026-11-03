package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

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

	_ = h.q.AddToWatchlist(r.Context(), db.AddToWatchlistParams{
		UserID:  user.ID,
		StockID: stockID,
	})
	// Market Watcher: 10 stocks on the watchlist.
	if n, _ := h.q.CountWatchlist(r.Context(), user.ID); n >= 10 {
		h.grantIfNew(r.Context(), user.ID, "watchlist_10")
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

	_ = h.q.RemoveFromWatchlist(r.Context(), db.RemoveFromWatchlistParams{
		UserID:  user.ID,
		StockID: stockID,
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
