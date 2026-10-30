package handler

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// pricesPage is /admin/prices: whether prices are up to date, and what the
// daily fetch and backfill did. Quote, profile, rate and news jobs log their
// errors to monitor, not here.
type pricesPage struct {
	MarketOpen bool
	LastQuote  string // "10:42 am ET", or "" before the first quote since the server started
	LatestDay  string // the newest bar of any stock
	Stocks     int
	UpToDate   int                        // stocks with a bar for LatestDay
	Behind     []db.ListStockLastDatesRow // the rest, oldest first
	Rate       string                     // AUD/USD, or "" with none stored
	RateDate   string
	Profiles   int   // profiles fresher than the weekly refresh
	Logos      int64 // logos pages can show
	Runs       []fetchRunRow
}

// fetchRunRow is a daily fetch or backfill, with how long it took.
type fetchRunRow struct {
	db.PriceFetchLog
	Took string // "0.4 s", "3 min"
}

// took says how long a run took, to a useful precision.
func took(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	if d < time.Minute {
		return fmt.Sprintf("%.1f s", d.Seconds())
	}
	return fmt.Sprintf("%d min", int(d.Round(time.Minute).Minutes()))
}

// AdminPrices shows whether prices are up to date.
func (h *Handler) AdminPrices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stocks, err := h.q.ListStockLastDates(ctx, "sp500")
	if err != nil {
		h.serverError(w, "stock last dates", err)
		return
	}
	p := pricesPage{Stocks: len(stocks)}
	for _, s := range stocks {
		p.LatestDay = max(p.LatestDay, s.LastDate)
	}
	for _, s := range stocks {
		if s.LastDate != "" && s.LastDate == p.LatestDay {
			p.UpToDate++
		} else {
			p.Behind = append(p.Behind, s)
		}
	}
	// Furthest behind first; the query already sorted them A–Z.
	slices.SortStableFunc(p.Behind, func(a, b db.ListStockLastDatesRow) int {
		return cmp.Compare(a.LastDate, b.LastDate)
	})

	at, open := market.QuoteStatus()
	p.MarketOpen = open
	if !at.IsZero() && market.NewYork != nil {
		p.LastQuote = at.In(market.NewYork).Format("3:04 pm ET, 2 Jan")
	}

	if rate, err := h.q.GetLatestExchangeRate(ctx); err == nil {
		p.Rate = fmt.Sprintf("%.4f", float64(rate.RateAudUsd)/1_000_000)
		p.RateDate = rate.Date
	}

	stale, _ := h.q.ListStocksNeedingProfile(ctx, db.ListStocksNeedingProfileParams{
		Market: "sp500", UpdatedAt: market.ProfileCutoff(time.Now()),
	})
	p.Profiles = len(stocks) - len(stale)
	p.Logos, _ = h.q.CountStockProfilesWithLogo(ctx)
	runs, _ := h.q.ListFetchLogs(ctx)
	for _, run := range runs {
		p.Runs = append(p.Runs, fetchRunRow{PriceFetchLog: run, Took: took(run.DurationMs.Int64)})
	}

	h.render(w, r, "admin/prices", "", PageData{Title: "Prices", Item: p})
}
