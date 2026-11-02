package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// marketClosed reports whether a stock's market is closed. A variable so tests
// can open and close the market.
var marketClosed = market.MarketClosed

// queuesMarketOrder reports whether a market order in game g waits for the
// open. With after-hours trading, the default, it fills straight away at the
// last price instead.
func queuesMarketOrder(g db.Game, stockMarket string) bool {
	return g.AfterHoursTrading == 0 && marketClosed(stockMarket)
}

// tradeContext is what the order ticket needs to know about the user's
// position in one stock.
type tradeContext struct {
	Participant  db.Participant
	Holding      db.Holding
	HoldingValue int64  // in the game's base currency
	ExchangeRate int64  // AUD/USD * 1,000,000; 1,000,000 when the currencies match
	Queued       bool   // the market is closed, so a market order queues for the open
	Closed       bool   // the market is closed, so a market order fills at the last price
	Opens        string // when a closed market opens, from marketOpens
}

// loadTradeContext returns the ticket data, or false if the user isn't in
// the game.
func (h *Handler) loadTradeContext(ctx context.Context, game db.Game, stock db.Stock, latest db.StockPrice, userID int64) (*tradeContext, bool) {
	participant, err := h.q.GetParticipantByGameAndUser(ctx, db.GetParticipantByGameAndUserParams{
		GameID: game.ID, UserID: userID,
	})
	if err != nil {
		return nil, false
	}
	tc := &tradeContext{Participant: participant, ExchangeRate: 1000000, Queued: queuesMarketOrder(game, stock.Market)}
	tc.Closed = !tc.Queued && marketClosed(stock.Market)
	if tc.Queued || tc.Closed {
		tc.Opens = marketOpens(marketNow())
	}
	tc.Holding, _ = h.q.GetHolding(ctx, db.GetHoldingParams{
		ParticipantID: participant.ID, StockID: stock.ID,
	})
	if stock.Currency != game.BaseCurrency {
		if rate, err := h.q.GetLatestExchangeRate(ctx); err == nil {
			tc.ExchangeRate = rate.RateAudUsd
		}
	}
	if tc.Holding.ID != 0 && latest.ID != 0 {
		tc.HoldingValue = convertCurrency(tc.Holding.Quantity*latest.Close, stock.Currency, game.BaseCurrency, tc.ExchangeRate)
	}
	return tc, true
}

// TradeExecute places an order from the stock page's ticket, or the home
// page's Close position button. The work is PlaceOrder's.
func (h *Handler) TradeExecute(w http.ResponseWriter, r *http.Request) {
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

	back := fmt.Sprintf("/games/%d/stocks/%d", gameID, stockID)
	r.ParseForm()
	fromHome := r.FormValue("from") == "home"
	if fromHome {
		// The Close position button on the game's home page.
		back = fmt.Sprintf("/games/%d", gameID)
	}
	side := r.FormValue("side") // "buy" or "sell"
	if side == "" {
		side = r.FormValue("type") // the old trade page's field
	}
	o := Order{
		UserID:   user.ID,
		GameID:   gameID,
		StockID:  stockID,
		Side:     side,
		Limit:    r.FormValue("order_type") == "limit",
		CloseAll: r.FormValue("all") == "1",
	}
	o.Quantity, _ = strconv.ParseInt(r.FormValue("quantity"), 10, 64)
	if o.Limit {
		back = fmt.Sprintf("/games/%d/stocks/%d", gameID, stockID)
		o.LimitPrice = parseDollars(r.FormValue("limit_price"))
		o.ExpiresAt = r.FormValue("expires_at")
	}
	h.placeFromForm(w, r, o, back, fromHome)
}

// placeFromForm places o for a form post and answers it: the ticket is drawn
// again with a confirmation, or a refusal is a toast or flash. back is where
// the order came from; a waiting order goes to the Orders tab, except a
// queued one from the home page, which stays there.
func (h *Handler) placeFromForm(w http.ResponseWriter, r *http.Request, o Order, back string, fromHome bool) {
	res, err := h.PlaceOrder(r.Context(), o)
	var refused *OrderError
	switch {
	case errors.Is(err, errNotFound):
		http.NotFound(w, r)
		return
	case errors.As(err, &refused):
		tradeError(w, r, refused.Msg, back)
		return
	case err != nil:
		slog.Error("place order", "user", o.UserID, "game", o.GameID, "stock", o.StockID, "error", err)
		tradeError(w, r, "Trade failed", back)
		return
	}
	game, _ := h.q.GetGame(r.Context(), o.GameID)
	done := back
	if res.Status != orderFilled && !(res.Status == orderQueued && fromHome) {
		done = fmt.Sprintf("/games/%d/orders", o.GameID)
	}
	h.orderPlaced(w, r, game, res.Stock, tradeDone{Message: res.Message, ToOrders: res.Status != orderFilled}, done)
}

// parseDollars reads a dollar amount, such as "12.34", as cents. Anything
// else is 0.
func parseDollars(s string) int64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 || f > math.MaxInt64/100 {
		return 0
	}
	return int64(math.Round(f * 100))
}

// tradeError refuses an order. The stock page's ticket posts through htmx
// and gets a toast, with the ticket left as it was (204 doesn't swap). A
// plain form post, such as the home page's Close position, gets a flash and
// goes back.
func tradeError(w http.ResponseWriter, r *http.Request, msg, back string) {
	if isHTMX(r) {
		triggerToast(w, msg, "error")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	setFlashCookie(w, msg, "error")
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// tradeDone is the confirmation at the top of the ticket after an order.
type tradeDone struct {
	Message  string
	ToOrders bool // a limit or queued order, waiting on the Orders tab
}

// orderPlaced finishes a successful order. The ticket (htmx) is drawn again
// in place with the confirmation and the new position, and the game bar's
// cash with it, so the player stays on the stock page and nothing is added
// to the browser's history. A plain form post gets a flash and goes to done.
func (h *Handler) orderPlaced(w http.ResponseWriter, r *http.Request, game db.Game, stock db.Stock, d tradeDone, done string) {
	if !isHTMX(r) {
		setFlashCookie(w, d.Message, "success")
		http.Redirect(w, r, done, http.StatusSeeOther)
		return
	}
	extra := h.ticketData(r.Context(), game, stock, auth.UserFromContext(r.Context()).ID)
	extra["Done"] = &d
	extra["Back"] = backLink(r.FormValue("back"), r.Host, game.ID, stock.ID)
	h.render(w, r, "stocks/detail", "ticket-update", PageData{Item: stock, Extra: extra})
}

// closeQueued reports whether the player has a market order of this side in
// this stock waiting for the market to open.
func (h *Handler) closeQueued(ctx context.Context, participantID, stockID int64, side string) bool {
	orders, _ := h.q.ListOpenLimitOrders(ctx, participantID)
	for _, o := range orders {
		if o.StockID == stockID && o.Type == side && o.Kind == "market" {
			return true
		}
	}
	return false
}

// revalueParticipant recalculates portfolio_value for a single participant
// after a trade. The trade's own rate won't do: it's 1:1 for a stock in the
// game's currency, and other holdings may not be. Without a rate the value
// waits for the scheduler's next revaluation.
func (h *Handler) revalueParticipant(ctx context.Context, participantID int64, baseCurrency string) {
	holdings, err := h.q.ListHoldings(ctx, participantID)
	if err != nil {
		return
	}
	var rate int64
	if er, err := h.q.GetLatestExchangeRate(ctx); err == nil {
		rate = er.RateAudUsd
	}
	holdingsValue, ok := market.HoldingsValue(holdings, baseCurrency, rate)
	if !ok {
		return
	}
	p, err := h.q.GetParticipant(ctx, participantID)
	if err != nil {
		return
	}
	h.q.UpdatePortfolioValue(ctx, db.UpdatePortfolioValueParams{
		PortfolioValue: p.CashBalance + holdingsValue,
		ID:             participantID,
	})
}

// convertCurrency converts cents from one currency to another using the exchange rate.
// Rate is AUD/USD * 1,000,000 (e.g. 0.65 = 650000).
func convertCurrency(amount int64, from, to string, rateAUDUSD int64) int64 {
	if from == to {
		return amount
	}
	if from == "AUD" && to == "USD" {
		return (amount * rateAUDUSD) / 1000000
	}
	if from == "USD" && to == "AUD" {
		return (amount * 1000000) / rateAUDUSD
	}
	return amount
}
