package handler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
)

// screenRow is one stock on the stocks page. Prices are in the stock's own
// currency.
type screenRow struct {
	db.ListScreenerStocksRow
	Price    int64   // the latest close
	Today    change  // the latest close against the one before
	TodayPct float64 // Today as a fraction of the previous close, for sorting
	Turnover int64   // the latest bar's volume × close, in cents
	Spark    string  // polyline points for the last 30 days (sparkPoints)
	SparkUp  bool    // the last 30 days ended at or above where they started
	Holders  int64   // players in this game holding it; set for Favourites only
}

// Initial stands in for a missing logo.
func (r screenRow) Initial() string {
	if r.Symbol == "" {
		return ""
	}
	return r.Symbol[:1]
}

// logoURL returns a stored logo if the CSP lets pages show it (img-src in
// security.go), or "". Profiles fetched before Twelve Data still hold
// Finnhub logos, which it blocks.
func logoURL(u string) string {
	if strings.HasPrefix(u, "https://api.twelvedata.com/") {
		return u
	}
	return ""
}

// screenFilter is one chip on the stocks page.
type screenFilter struct {
	Key, Label, Caption string
}

// screenFilters are the chips, in order. The first is the default.
var screenFilters = []screenFilter{
	{"picks", "Rob's Picks", "Rob's Picks"},
	{"mag7", "Mag 7", "The Magnificent 7"},
	{"tech", "Tech", "Tech, biggest first"},
	{"up", "Gainers", "Today's biggest gainers"},
	{"down", "Losers", "Today's biggest losers"},
	{"big", "Biggest", "The biggest companies"},
	{"active", "Most traded", "Most traded today, by dollar value"},
	{"aus", "Aussie", "Australian companies listed in the US"},
	{"intl", "International", "Overseas companies listed in the US"},
	{"popular", "Players' favourites", "Most held by players in this game"},
	{"all", "All A–Z", "Every stock, A–Z"},
}

var mag7 = map[string]bool{"AAPL": true, "MSFT": true, "AMZN": true, "NVDA": true, "GOOGL": true, "META": true, "TSLA": true}

// How many rows the ranked chips show.
const (
	screenTop     = 25
	screenBiggest = 50
	searchLimit   = 20
)

// sparkDays is how far back the row charts go.
const sparkDays = 30

// screenTTL is how long the stocks page reuses its rows. Quotes update
// prices once a minute at most.
const screenTTL = time.Minute

// screenCache holds each market's rows, so tapping chips doesn't reread
// every stock's prices.
type screenCache struct {
	mu   sync.Mutex
	at   map[string]time.Time
	rows map[string][]screenRow
}

// clear drops the cached rows, after Rob's Picks change.
func (c *screenCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at, c.rows = nil, nil
}

// screenRows returns every stock in the market with its price, move and
// chart. Callers must copy the slice before reordering it.
func (h *Handler) screenRows(ctx context.Context, mkt string) ([]screenRow, error) {
	c := &h.screener
	c.mu.Lock()
	defer c.mu.Unlock()
	if at, ok := c.at[mkt]; ok && time.Since(at) < screenTTL {
		return c.rows[mkt], nil
	}

	stocks, err := h.q.ListScreenerStocks(ctx, db.ListScreenerStocksParams{Market: mkt, Column2: mkt})
	if err != nil {
		return nil, err
	}
	bars, err := h.q.ListRecentCloses(ctx)
	if err != nil {
		return nil, err
	}
	byStock := make(map[int64][]db.ListRecentClosesRow)
	for _, b := range bars {
		byStock[b.StockID] = append(byStock[b.StockID], b)
	}

	rows := make([]screenRow, len(stocks))
	for i, s := range stocks {
		rows[i] = newScreenRow(s, byStock[s.ID])
	}

	if c.at == nil {
		c.at, c.rows = map[string]time.Time{}, map[string][]screenRow{}
	}
	c.at[mkt], c.rows[mkt] = time.Now(), rows
	return rows, nil
}

// newScreenRow works out a stock's row from its recent bars, oldest first.
func newScreenRow(s db.ListScreenerStocksRow, bars []db.ListRecentClosesRow) screenRow {
	r := screenRow{ListScreenerStocksRow: s}
	r.Logo = logoURL(r.Logo)
	if len(bars) == 0 {
		return r
	}
	last := bars[len(bars)-1]
	prev := last.Close
	if len(bars) > 1 {
		prev = bars[len(bars)-2].Close
	}
	r.Price = last.Close
	r.Today = change{Amount: last.Close - prev, Base: prev}
	if prev != 0 {
		r.TodayPct = float64(last.Close-prev) / float64(prev)
	}
	r.Turnover = last.Volume * last.Close

	var closes []int64
	if t, err := time.Parse(time.DateOnly, last.Date); err == nil {
		from := t.AddDate(0, 0, -sparkDays).Format(time.DateOnly)
		for _, b := range bars {
			if b.Date >= from {
				closes = append(closes, b.Close)
			}
		}
	}
	r.Spark = sparkPoints(closes)
	r.SparkUp = len(closes) > 0 && closes[len(closes)-1] >= closes[0]
	return r
}

// sparkPoints draws closes as SVG polyline points in a 60×20 viewBox, with
// a point of room top and bottom for the line's width. Fewer than two
// closes draw nothing.
func sparkPoints(closes []int64) string {
	if len(closes) < 2 {
		return ""
	}
	lo, hi := closes[0], closes[0]
	for _, c := range closes {
		lo, hi = min(lo, c), max(hi, c)
	}
	pts := make([]string, len(closes))
	for i, c := range closes {
		x := float64(i) * 60 / float64(len(closes)-1)
		y := 10.0
		if hi > lo {
			y = 1 + float64(hi-c)/float64(hi-lo)*18
		}
		pts[i] = fmt.Sprintf("%.1f,%.1f", x, y)
	}
	return strings.Join(pts, " ")
}

// screenResult is what the stocks page lists.
type screenResult struct {
	Filter  string // the chip's key, or "" for a search
	Caption string
	Rows    []screenRow
}

// filterKey returns f if it names a chip, otherwise the default.
func filterKey(f string) string {
	for _, sf := range screenFilters {
		if sf.Key == f {
			return f
		}
	}
	return screenFilters[0].Key
}

// applyFilter picks and orders the rows for a chip and sector, or for a
// search, which looks through every stock. holders counts players holding
// each stock, for Favourites. It never reorders rows itself.
func applyFilter(rows []screenRow, f, sector, q string, holders map[int64]int64) screenResult {
	if q = strings.TrimSpace(q); q != "" {
		return searchRows(rows, q)
	}
	f = filterKey(f)
	res := screenResult{Filter: f}
	for _, sf := range screenFilters {
		if sf.Key == f {
			res.Caption = sf.Caption
		}
	}
	if sector != "" {
		res.Caption += " in " + sector
	}

	keep := func(r screenRow) bool {
		if sector != "" && r.Sector != sector {
			return false
		}
		switch f {
		case "picks":
			return r.IsPick == 1
		case "mag7":
			return r.Market == "sp500" && mag7[r.Symbol]
		case "tech":
			return r.Sector == "Information Technology"
		case "up":
			return r.Today.Amount > 0
		case "down":
			return r.Today.Amount < 0
		case "active":
			return r.Turnover > 0
		case "aus":
			return market.IsAusUS(r.Symbol, r.Market)
		case "intl":
			return market.IsOtherUS(r.Symbol, r.Market)
		case "popular":
			return holders[r.ID] > 0
		}
		return true
	}
	var out []screenRow
	for _, r := range rows {
		if keep(r) {
			if f == "popular" {
				r.Holders = holders[r.ID]
			}
			out = append(out, r)
		}
	}

	byCap := func(i, j int) bool { return out[i].MarketCap > out[j].MarketCap }
	limit := 0
	switch f {
	case "picks":
		sort.SliceStable(out, func(i, j int) bool { return out[i].PickOrder < out[j].PickOrder })
	case "mag7", "tech", "aus", "intl":
		sort.SliceStable(out, byCap)
	case "big":
		sort.SliceStable(out, byCap)
		limit = screenBiggest
	case "up":
		sort.SliceStable(out, func(i, j int) bool { return out[i].TodayPct > out[j].TodayPct })
		limit = screenTop
	case "down":
		sort.SliceStable(out, func(i, j int) bool { return out[i].TodayPct < out[j].TodayPct })
		limit = screenTop
	case "active":
		sort.SliceStable(out, func(i, j int) bool { return out[i].Turnover > out[j].Turnover })
		limit = screenTop
	case "popular":
		sort.SliceStable(out, func(i, j int) bool { return out[i].Holders > out[j].Holders })
		limit = screenTop
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	res.Rows = out
	return res
}

// searchRows finds stocks whose symbol or name contains q: an exact symbol
// first, then symbols starting with q, then the rest A–Z.
func searchRows(rows []screenRow, q string) screenResult {
	uq := strings.ToUpper(q)
	rank := func(r screenRow) int {
		switch {
		case r.Symbol == uq:
			return 0
		case strings.HasPrefix(r.Symbol, uq):
			return 1
		}
		return 2
	}
	var out []screenRow
	for _, r := range rows {
		if strings.Contains(r.Symbol, uq) || strings.Contains(strings.ToUpper(r.Name), uq) {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	if len(out) > searchLimit {
		out = out[:searchLimit]
	}
	return screenResult{Caption: "Results for “" + q + "”", Rows: out}
}

// screenSectors lists the sectors in rows, A–Z, for the sector menu.
func screenSectors(rows []screenRow) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range rows {
		if r.Sector != "" && !seen[r.Sector] {
			seen[r.Sector] = true
			out = append(out, r.Sector)
		}
	}
	sort.Strings(out)
	return out
}
