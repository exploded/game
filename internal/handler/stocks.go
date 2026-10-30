package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// peerLink is a resolved peer symbol used by the stock detail template. If
// StockID is zero, the symbol is not in our universe and is rendered as a
// plain badge instead of a link.
type peerLink struct {
	Symbol  string
	StockID int64
}

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

	// Show all available price history: daily for the last two years, weekly
	// before that.
	now := time.Now().UTC()
	prices, _ := h.q.ListPriceHistory(r.Context(), db.ListPriceHistoryParams{
		StockID: stock.ID,
		Date:    "2000-01-01",
		Date_2:  now.Format("2006-01-02"),
	})
	prices = chartBars(prices, now.AddDate(-2, 0, 0).Format("2006-01-02"))

	latestPrice, _ := h.q.GetLatestPrice(r.Context(), stock.ID)
	profile, _ := h.q.GetStockProfile(r.Context(), stock.ID)
	news, _ := h.q.ListStockNews(r.Context(), db.ListStockNewsParams{StockID: stock.ID, Limit: 20})
	recs, _ := h.q.ListStockRecommendations(r.Context(), db.ListStockRecommendationsParams{StockID: stock.ID, Limit: 6})

	// Split the comma-separated peers blob and resolve each peer symbol to a
	// stock ID in the same market so the template can link to stock detail
	// pages directly. ASX peer symbols may carry a ".AX" suffix we need to
	// strip before the DB lookup. Peers not in our stock universe are still
	// rendered, just as plain badges.
	var peers []peerLink
	if profile.Peers != "" {
		for _, p := range strings.Split(profile.Peers, ",") {
			sym := strings.TrimSpace(p)
			if sym == "" {
				continue
			}
			lookup := strings.TrimSuffix(sym, ".AX")
			var stockID int64
			if peer, err := h.q.GetStockBySymbol(r.Context(), db.GetStockBySymbolParams{
				Symbol: lookup, Market: stock.Market,
			}); err == nil {
				stockID = peer.ID
			}
			peers = append(peers, peerLink{Symbol: lookup, StockID: stockID})
		}
	}

	// The order ticket shows only for players in the game; anyone can view.
	trade, _ := h.loadTradeContext(r.Context(), game, stock, latestPrice, auth.UserFromContext(r.Context()).ID)

	// Rob's videos that talk about this stock (picked by the classifier).
	robVideos, _ := h.q.ListStockVideos(r.Context(), stock.ID)

	h.render(w, r, "stocks/detail", "", PageData{
		Title: stock.Symbol + " - " + stock.Name,
		Item:  stock,
		Items: prices,
		Extra: map[string]any{
			"Game":            game,
			"PriceStats":      h.loadPriceStats(r, gameID, stock.ID),
			"LatestPrice":     latestPrice,
			"Profile":         profile,
			"News":            news,
			"Recommendations": recs,
			"Peers":           peers,
			"Trade":           trade,
			"RobVideos":       robVideos,
		},
	})
}

// priceStats is the data for the price header (stocks/_price_stats.html).
type priceStats struct {
	GameID, StockID int64
	Latest          db.StockPrice
	ChangeText      string // e.g. "+$3.27 (+0.95%)" against the previous close
	Up              bool
	Open            bool   // US market open; the header polls for updates while true
	AsOf            string // "As of 10:42 am ET" while open, otherwise "Close, 28 Oct"
}

// loadPriceStats builds the price header from the newest two price rows. While
// the market is open, intraday quotes keep the newest row current, so this
// reads only the database and costs no Twelve Data credits.
func (h *Handler) loadPriceStats(r *http.Request, gameID, stockID int64) priceStats {
	ps := priceStats{GameID: gameID, StockID: stockID}
	rows, err := h.q.ListLatestPrices(r.Context(), stockID)
	if err != nil || len(rows) == 0 {
		return ps
	}
	ps.Latest = rows[0]
	prev := rows[0].Close
	if len(rows) > 1 {
		prev = rows[1].Close
	}
	change := ps.Latest.Close - prev
	ps.Up = change >= 0
	sign, abs := "+", change
	if change < 0 {
		sign, abs = "-", -change
	}
	var pct float64
	if prev != 0 {
		pct = float64(abs) / float64(prev) * 100
	}
	ps.ChangeText = fmt.Sprintf("%s%s (%s%.2f%%)", sign, fmtCents(abs), sign, pct)

	at, open := market.QuoteStatus()
	ps.Open = open && market.NewYork != nil
	if ps.Open {
		ps.AsOf = "As of " + at.In(market.NewYork).Format("3:04 pm") + " ET"
	} else if d, err := time.Parse("2006-01-02", ps.Latest.Date); err == nil {
		ps.AsOf = "Close, " + d.Format("2 Jan")
	}
	return ps
}

// StockPrice renders just the price header. The stock page polls it every
// minute while the US market is open.
func (h *Handler) StockPrice(w http.ResponseWriter, r *http.Request) {
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
	if !isHTMX(r) {
		http.Redirect(w, r, fmt.Sprintf("/games/%d/stocks/%d", gameID, stockID), http.StatusSeeOther)
		return
	}
	h.render(w, r, "stocks/detail", "price-stats", PageData{
		Extra: map[string]any{"PriceStats": h.loadPriceStats(r, gameID, stockID)},
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

	// An empty query (the search box cleared) shows the full list again.
	var stocks []db.Stock
	if q := r.URL.Query().Get("q"); q == "" {
		stocks, _ = h.q.ListStocksByMarket(r.Context(), db.ListStocksByMarketParams{
			Market: game.Markets, Column2: game.Markets,
		})
	} else {
		pattern := "%" + q + "%"
		stocks, _ = h.q.SearchStocks(r.Context(), db.SearchStocksParams{
			Market:  game.Markets,
			Column2: game.Markets,
			Symbol:  pattern,
			Name:    pattern,
		})
	}

	h.render(w, r, "stocks/browse", "stock-rows", PageData{Item: game, Items: stocks})
}
