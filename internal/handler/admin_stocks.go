package handler

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// /admin/stocks: add a stock by ticker, retire or bring back a stock, move
// a stock between lists (S&P 500, other US, Australian, international), and
// the new NYSE and Nasdaq listings market.CheckListings has found. Since
// 19 November 2026 this replaces editing internal/market/symbols.go and
// deploying.

// stocksPage is /admin/stocks.
type stocksPage struct {
	Search   string
	Stocks   []db.SearchStocksAdminRow
	Lookup   string             // the ticker looked up, if any
	Found    *market.FoundStock // what Twelve Data knows about it
	FoundErr string             // why the lookup failed
	Existing *db.Stock          // the stock, if it's already in the game
	New      []newListingRow    // new listings, biggest first
	Sectors  []string           // for the add form
	Listings []struct{ Key, Label string }
	SPSize   string // market.SPSizeCap, "US$20.0 billion"
}

// newListingRow is a new listing, with its market cap written out.
type newListingRow struct {
	db.ListNewListingsRow
	Cap    string // "US$61.5 billion", or "" until known
	SPSize bool   // at or above market.SPSizeCap
}

// newListingDays is how far back the page lists new listings.
const newListingDays = 60

// stockLookup asks Twelve Data about a ticker. Tests replace it.
var stockLookup = func(ctx context.Context, h *Handler, symbol string) (market.FoundStock, error) {
	return market.NewFetcher(h.q, h.rawDB).LookupStock(ctx, symbol)
}

// fetchNewStock fetches a just-added stock's prices and profile in the
// background. Tests replace it.
var fetchNewStock = func(h *Handler, s db.Stock) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		market.NewFetcher(h.q, h.rawDB).FetchNewStock(ctx, h.q, s)
	}()
}

// tickerRe is what a NYSE or Nasdaq ticker can look like: BRK.B, BF-B.
var tickerRe = regexp.MustCompile(`^[A-Z][A-Z0-9.\-]{0,9}$`)

// AdminStocks shows /admin/stocks. ?q= searches the stocks; ?lookup= asks
// Twelve Data about a ticker and shows the add form.
func (h *Handler) AdminStocks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p := stocksPage{
		Search:   strings.TrimSpace(r.URL.Query().Get("q")),
		Sectors:  market.Sectors,
		Listings: market.Listings,
		SPSize:   market.Billions(market.SPSizeCap),
	}
	var err error
	p.Stocks, err = h.q.SearchStocksAdmin(ctx, p.Search)
	if err != nil {
		h.serverError(w, "search stocks", err)
		return
	}

	if t := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("lookup"))); t != "" {
		p.Lookup = t
		switch {
		case !tickerRe.MatchString(t):
			p.FoundErr = "That doesn't look like a ticker."
		default:
			if s, err := h.q.GetStockBySymbol(ctx, db.GetStockBySymbolParams{Symbol: t, Market: "sp500"}); err == nil {
				p.Existing = &s
			} else if f, err := stockLookup(ctx, h, t); errors.Is(err, market.ErrNoSuchStock) {
				p.FoundErr = "Twelve Data has no NYSE or Nasdaq stock " + t + ". Check the ticker."
			} else if err != nil {
				slog.Warn("stock lookup", "symbol", t, "error", err)
				p.FoundErr = "Couldn't reach Twelve Data. Try again in a minute."
			} else {
				p.Found = &f
			}
		}
	}

	since := time.Now().In(nyOrUTC()).AddDate(0, 0, -newListingDays).Format(time.DateOnly)
	rows, err := h.q.ListNewListings(ctx, since)
	if err != nil {
		h.serverError(w, "new listings", err)
		return
	}
	for _, l := range rows {
		row := newListingRow{ListNewListingsRow: l, SPSize: l.MarketCap >= market.SPSizeCap}
		if l.MarketCap > 0 {
			row.Cap = market.Billions(l.MarketCap)
		}
		p.New = append(p.New, row)
	}

	h.render(w, r, "admin/stocks", "", PageData{Title: "Stocks", Item: p})
}

// nyOrUTC is New York's time zone, which listing dates use, or UTC.
func nyOrUTC() *time.Location {
	if market.NewYork != nil {
		return market.NewYork
	}
	return time.UTC
}

// AdminStockAdd adds a stock from the form the lookup filled in, then
// fetches its prices and profile in the background.
func (h *Handler) AdminStockAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	symbol := strings.ToUpper(strings.TrimSpace(r.FormValue("symbol")))
	name := strings.TrimSpace(r.FormValue("name"))
	sector := r.FormValue("sector")
	listing := r.FormValue("listing")
	if msg := stockFormError(symbol, name, sector, listing); msg != "" {
		setFlashCookie(w, msg, "error")
		http.Redirect(w, r, "/admin/stocks?lookup="+url.QueryEscape(symbol), http.StatusSeeOther)
		return
	}
	if _, err := h.q.GetStockBySymbol(ctx, db.GetStockBySymbolParams{Symbol: symbol, Market: "sp500"}); err == nil {
		setFlashCookie(w, symbol+" is already in the game.", "error")
		http.Redirect(w, r, "/admin/stocks?q="+url.QueryEscape(symbol), http.StatusSeeOther)
		return
	} else if !errors.Is(err, sql.ErrNoRows) {
		h.serverError(w, "find stock", err)
		return
	}
	if err := h.q.InsertStock(ctx, db.InsertStockParams{
		Symbol: symbol, Name: name, Market: "sp500", Sector: sector, Currency: "USD", Listing: listing,
	}); err != nil {
		h.serverError(w, "add stock", err)
		return
	}
	s, err := h.q.GetStockBySymbol(ctx, db.GetStockBySymbolParams{Symbol: symbol, Market: "sp500"})
	if err != nil {
		h.serverError(w, "find added stock", err)
		return
	}
	if err := market.LoadListings(ctx, h.q); err != nil {
		slog.Error("load listings", "error", err)
	}
	fetchNewStock(h, s)
	slog.Info("stock added", "symbol", symbol, "listing", listing)
	setFlashCookie(w, "Added "+market.StockLabel(symbol, name)+
		". Its prices are being fetched now. If it hasn't traded yet, they arrive with its first trade.", "success")
	http.Redirect(w, r, "/admin/stocks?q="+url.QueryEscape(symbol), http.StatusSeeOther)
}

// stockFormError says what's wrong with the add form, or "".
func stockFormError(symbol, name, sector, listing string) string {
	switch {
	case !tickerRe.MatchString(symbol):
		return "That doesn't look like a ticker."
	case name == "" || len(name) > 120:
		return "Give the company a name (up to 120 characters)."
	case !slices.Contains(market.Sectors, sector):
		return "Pick a sector."
	case !market.ValidListing(listing):
		return "Pick a list."
	}
	return ""
}

// adminStock loads the stock named in the path.
func (h *Handler) adminStock(w http.ResponseWriter, r *http.Request) (db.Stock, bool) {
	id, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return db.Stock{}, false
	}
	s, err := h.q.GetStock(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return db.Stock{}, false
	}
	return s, true
}

// stocksBack is where a stock action returns: the search it came from.
func stocksBack(r *http.Request) string {
	if q := strings.TrimSpace(r.FormValue("q")); q != "" {
		return "/admin/stocks?q=" + url.QueryEscape(q)
	}
	return "/admin/stocks"
}

// AdminStockActive retires a stock (hidden from players, its past trades
// kept) or brings one back. A stock anyone holds or has an order in can't
// be retired: it would sit at its last price.
func (h *Handler) AdminStockActive(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s, ok := h.adminStock(w, r)
	if !ok {
		return
	}
	label := market.StockLabel(s.Symbol, s.Name)
	if s.IsActive == 1 {
		n, err := h.q.CountStockExposure(ctx, s.ID)
		if err != nil {
			h.serverError(w, "stock exposure", err)
			return
		}
		if n.Holders > 0 || n.Orders > 0 {
			setFlashCookie(w, label+" can't be retired: "+strconv.FormatInt(n.Holders, 10)+
				" players hold it and "+strconv.FormatInt(n.Orders, 10)+" orders are waiting in it.", "error")
			http.Redirect(w, r, stocksBack(r), http.StatusSeeOther)
			return
		}
		if err := h.q.SetStockActive(ctx, db.SetStockActiveParams{IsActive: 0, ID: s.ID}); err != nil {
			h.serverError(w, "retire stock", err)
			return
		}
		slog.Info("stock retired", "symbol", s.Symbol)
		setFlashCookie(w, "Retired "+label+". Players can't find or trade it, and past trades still name it.", "success")
	} else {
		if err := h.q.SetStockActive(ctx, db.SetStockActiveParams{IsActive: 1, ID: s.ID}); err != nil {
			h.serverError(w, "bring back stock", err)
			return
		}
		slog.Info("stock brought back", "symbol", s.Symbol)
		setFlashCookie(w, "Brought back "+label+".", "success")
	}
	http.Redirect(w, r, stocksBack(r), http.StatusSeeOther)
}

// AdminStockListing moves a stock to another list, such as from other US
// to the S&P 500 when S&P adds it to the index.
func (h *Handler) AdminStockListing(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	s, ok := h.adminStock(w, r)
	if !ok {
		return
	}
	listing := r.FormValue("listing")
	if !market.ValidListing(listing) {
		http.Error(w, "unknown list", http.StatusBadRequest)
		return
	}
	if err := h.q.SetStockListing(ctx, db.SetStockListingParams{Listing: listing, ID: s.ID}); err != nil {
		h.serverError(w, "set listing", err)
		return
	}
	if err := market.LoadListings(ctx, h.q); err != nil {
		slog.Error("load listings", "error", err)
	}
	slog.Info("stock listing changed", "symbol", s.Symbol, "listing", listing)
	setFlashCookie(w, "Moved "+market.StockLabel(s.Symbol, s.Name)+" to "+listingLabel(listing)+".", "success")
	// To a search for it: moved to the S&P 500, it would leave the default list.
	http.Redirect(w, r, "/admin/stocks?q="+url.QueryEscape(s.Symbol)+"#stocks", http.StatusSeeOther)
}

// listingLabel is a listing's name for admins.
func listingLabel(key string) string {
	for _, l := range market.Listings {
		if l.Key == key {
			return l.Label
		}
	}
	return key
}

// AdminListingDismiss hides a new listing an admin doesn't want to add.
func (h *Handler) AdminListingDismiss(w http.ResponseWriter, r *http.Request) {
	symbol, exchange := r.FormValue("symbol"), r.FormValue("exchange")
	if err := h.q.DismissListing(r.Context(), db.DismissListingParams{Symbol: symbol, Exchange: exchange}); err != nil {
		h.serverError(w, "dismiss listing", err)
		return
	}
	setFlashCookie(w, "Dismissed "+symbol+".", "success")
	http.Redirect(w, r, "/admin/stocks#new", http.StatusSeeOther)
}
