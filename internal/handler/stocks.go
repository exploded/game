package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) StocksBrowse(w http.ResponseWriter, r *http.Request) {
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

	// Verify user is participant.
	_, err = h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		setFlashCookie(w, "Join the game first", "error")
		http.Redirect(w, r, "/games/"+r.PathValue("id"), http.StatusSeeOther)
		return
	}

	market := game.Markets
	stocks, _ := h.q.ListStocksByMarket(r.Context(), db.ListStocksByMarketParams{
		Market: market, Column2: market,
	})

	h.render(w, r, "stocks/browse", "", PageData{
		Title: "Stocks - " + game.Name,
		Item:  game,
		Items: stocks,
	})
}

func (h *Handler) StockDetail(w http.ResponseWriter, r *http.Request) {
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

	game, err := h.q.GetGame(r.Context(), gameID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	stock, err := h.q.GetStock(r.Context(), stockID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Show all available price history (last 90 days), not just game date range.
	now := time.Now().UTC()
	prices, _ := h.q.ListPriceHistory(r.Context(), db.ListPriceHistoryParams{
		StockID: stock.ID,
		Date:    now.AddDate(0, 0, -90).Format("2006-01-02"),
		Date_2:  now.Format("2006-01-02"),
	})

	latestPrice, _ := h.q.GetLatestPrice(r.Context(), stock.ID)

	h.render(w, r, "stocks/detail", "", PageData{
		Title: stock.Symbol + " - " + stock.Name,
		Item:  stock,
		Items: prices,
		Extra: map[string]any{
			"Game":        game,
			"LatestPrice": latestPrice,
		},
	})
}

func (h *Handler) StockSearch(w http.ResponseWriter, r *http.Request) {
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

	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(""))
		return
	}

	pattern := "%" + q + "%"
	stocks, _ := h.q.SearchStocks(r.Context(), db.SearchStocksParams{
		Market:   game.Markets,
		Column2: game.Markets,
		Symbol:   pattern,
		Name:     pattern,
	})

	w.Header().Set("Content-Type", "text/html")
	for _, s := range stocks {
		latestPrice, _ := h.q.GetLatestPrice(r.Context(), s.ID)
		priceStr := "N/A"
		if latestPrice.ID != 0 {
			priceStr = fmtCents(latestPrice.Close)
		}
		w.Write([]byte(`<tr>
			<td><a href="/games/` + r.PathValue("id") + `/stocks/` + strconv.FormatInt(s.ID, 10) + `">` + s.Symbol + `</a></td>
			<td>` + s.Name + `</td>
			<td>` + s.Market + `</td>
			<td>` + priceStr + `</td>
			<td><a href="/games/` + r.PathValue("id") + `/trade/` + strconv.FormatInt(s.ID, 10) + `" class="btn btn-primary btn-sm">Trade</a></td>
		</tr>`))
	}
}
