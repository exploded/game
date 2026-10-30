package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
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

// StocksBrowse is the stocks page: chips (screenFilters) and a sector menu
// pick what it lists, or a search looks through every stock. An htmx
// request gets just the list.
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
	participant, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		setFlashCookie(w, "Join the game first", "error")
		http.Redirect(w, r, "/games/"+r.PathValue("id"), http.StatusSeeOther)
		return
	}

	rows, err := h.screenRows(r.Context(), game.Markets)
	if err != nil {
		h.serverError(w, "stocks page", err)
		return
	}
	v := r.URL.Query()
	f, sector, search := filterKey(v.Get("f")), v.Get("sector"), strings.TrimSpace(v.Get("q"))
	var holders map[int64]int64
	if f == "popular" && search == "" {
		holders = h.holderCounts(r, gameID)
	}
	res := applyFilter(rows, f, sector, search, holders)
	h.markRows(r.Context(), participant.ID, res.Rows)

	data := PageData{
		Title: "Stocks - " + game.Name,
		Item:  game,
		Items: res.Rows,
		Extra: map[string]any{
			"Result":    res,
			"Filter":    f,
			"Filters":   screenFilters,
			"Sectors":   screenSectors(rows),
			"Sector":    sector,
			"Q":         search,
			"ShowNotes": res.Filter == "picks",
		},
	}
	// The same URL serves the page and the list, so caches must tell them
	// apart. Back re-fetches the page with HX-Request set, and needs it all.
	w.Header().Add("Vary", "HX-Request")
	if isHTMX(r) && r.Header.Get("HX-History-Restore-Request") != "true" {
		h.render(w, r, "stocks/browse", "stock-list", data)
		return
	}
	h.render(w, r, "stocks/browse", "", data)
}

// holderCounts maps each stock to how many players in the game hold it.
func (h *Handler) holderCounts(r *http.Request, gameID int64) map[int64]int64 {
	counts, _ := h.q.ListGameHolderCounts(r.Context(), gameID)
	m := make(map[int64]int64, len(counts))
	for _, c := range counts {
		m[c.StockID] = c.Holders
	}
	return m
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

	profile, _ := h.q.GetStockProfile(r.Context(), stock.ID)
	profile.Logo = logoURL(profile.Logo)
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

	// Rob's videos that talk about this stock (picked by the classifier).
	robVideos, _ := h.q.ListStockVideos(r.Context(), stock.ID)
	var pick *db.StockPick
	if p, err := h.q.GetStockPick(r.Context(), stock.ID); err == nil {
		pick = &p
	}

	extra := h.ticketData(r.Context(), game, stock, auth.UserFromContext(r.Context()).ID)
	extra["Back"] = backLink(r.Referer(), r.Host, gameID, stock.ID)
	extra["PriceStats"] = h.loadPriceStats(r, gameID, stock.ID)
	extra["Profile"] = profile
	extra["News"] = news
	extra["Recommendations"] = recs
	extra["Peers"] = peers
	extra["RobVideos"] = robVideos
	extra["Pick"] = pick
	h.render(w, r, "stocks/detail", "", PageData{
		Title: stock.Symbol + " - " + stock.Name,
		Item:  stock,
		Items: prices,
		Extra: extra,
	})
}

// ticketData is what the order ticket (stocks/_ticket.html) needs. The
// ticket shows only for players in the game; anyone can view the stock.
func (h *Handler) ticketData(ctx context.Context, game db.Game, stock db.Stock, userID int64) map[string]any {
	latest, _ := h.q.GetLatestPrice(ctx, stock.ID)
	trade, _ := h.loadTradeContext(ctx, game, stock, latest, userID)
	var watching bool
	if trade != nil {
		n, _ := h.q.IsWatching(ctx, db.IsWatchingParams{ParticipantID: trade.Participant.ID, StockID: stock.ID})
		watching = n > 0
	}
	return map[string]any{
		"Game":        game,
		"LatestPrice": latest,
		"Trade":       trade,
		"Watching":    watching,
	}
}

// backTo is where the stock page's Back link goes.
type backTo struct {
	URL   string
	Label string // "Stocks", "Portfolio", ..., or "Back" for another page
}

// backLink is the page in this game that the player opened the stock from,
// read from ref (the Referer, or the ticket's back field): the stock list
// with its chip and search, their portfolio, and so on. Anything else, such
// as another site or this same stock, goes to the stock list. app.js goes
// back through the browser's history when it can, so the list keeps its
// scroll position.
func backLink(ref, host string, gameID, stockID int64) backTo {
	game := fmt.Sprintf("/games/%d", gameID)
	fallback := backTo{URL: game + "/stocks", Label: "Stocks"}
	u, err := url.Parse(ref)
	if err != nil || ref == "" || (u.Host != "" && u.Host != host) || (u.Host == "" && u.Scheme != "") {
		return fallback
	}
	rest, ok := strings.CutPrefix(u.Path, game)
	if !ok || (rest != "" && rest[0] != '/') {
		return fallback
	}
	link := u.Path
	if u.RawQuery != "" {
		link += "?" + u.RawQuery
	}
	parts := strings.Split(strings.Trim(rest, "/"), "/")
	switch {
	case rest == "" || rest == "/":
		return backTo{URL: link, Label: "Portfolio"}
	case len(parts) == 1 && parts[0] == "stocks":
		return backTo{URL: link, Label: "Stocks"}
	case len(parts) == 1 && parts[0] == "watchlist":
		return backTo{URL: link, Label: "Watchlist"}
	case len(parts) == 1 && parts[0] == "orders":
		return backTo{URL: link, Label: "Orders"}
	case parts[0] == "trade" || (parts[0] == "stocks" && len(parts) == 2 && parts[1] == strconv.FormatInt(stockID, 10)):
		return fallback // this stock itself
	}
	return backTo{URL: link, Label: "Back"}
}

// priceStats is the data for the price header (stocks/_price_stats.html).
type priceStats struct {
	GameID, StockID int64
	Latest          db.StockPrice
	ChangeText      string // e.g. "+$3.27 (+0.95%)" against the previous close
	Up              bool
	Open            bool   // US market open; the header polls for updates while true
	AsOf            string // "As of 10:42 am ET" while open, otherwise "Close, 28 Oct"
	Closes          string // "Closes in 2 hr 5 min" while open
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
		ps.Closes = marketCloses(marketNow())
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

// StockSearch fills the search box on an empty portfolio with stock rows.
// An empty query shows Rob's Picks.
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

	rows, err := h.screenRows(r.Context(), game.Markets)
	if err != nil {
		h.serverError(w, "stock search", err)
		return
	}
	res := applyFilter(rows, "", "", r.URL.Query().Get("q"), nil)
	if p, err := h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: auth.UserFromContext(r.Context()).ID,
	}); err == nil {
		h.markRows(r.Context(), p.ID, res.Rows)
	}
	h.render(w, r, "stocks/browse", "stock-rows", PageData{Item: game, Items: res.Rows})
}
