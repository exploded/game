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
	Held     int64   // the player's position, negative for a short; set by markRows
	Watching bool    // on the player's watchlist; set by markRows

	Health    int    // the health score, 0 to 5 (healthScore)
	HasHealth bool   // false for banks and insurers, and stocks with no financials
	Figure    string // what a financial chip matched on; set by applyFilter
}

// HeldShares is the player's position without its sign.
func (r screenRow) HeldShares() int64 {
	if r.Held < 0 {
		return -r.Held
	}
	return r.Held
}

// Initial stands in for a missing logo.
func (r screenRow) Initial() string {
	if r.Symbol == "" {
		return ""
	}
	return r.Symbol[:1]
}

// logoURL returns a stored logo if the CSP lets pages show it (img-src in
// security.go), or "". Twelve Data serves logos from two hosts (Meta's and
// SpaceX's are on logo.twelvedata.com). Profiles fetched before Twelve Data
// still hold Finnhub logos, which it blocks.
func logoURL(u string) string {
	if strings.HasPrefix(u, "https://api.twelvedata.com/") || strings.HasPrefix(u, "https://logo.twelvedata.com/") {
		return u
	}
	return ""
}

// defaultFilter is the chip the stocks page opens on.
const defaultFilter = "picks"

// firstFinFilter is the first chip that tests a company's financials. The
// stocks page puts a divider before it.
const firstFinFilter = "profit"

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
	r.Health, r.HasHealth = healthScore(r)
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

// The financial chips and the health score read stock_profiles, from Twelve
// Data /statistics. It stores 0 for a figure Twelve Data didn't send, so
// every test needs a real value, not just a zero that passes "under 15".

// hasFinancials says whether a stock has figures at all: funds such as IBIT
// have none.
func hasFinancials(r screenRow) bool { return r.Revenue != 0 }

// balanceSheetFits says whether debt-to-equity, the current ratio and cash
// against debt mean anything for a stock. They don't for banks and
// insurers, whose deposits and policies are debts and whose cash is
// customers' money.
func balanceSheetFits(r screenRow) bool {
	return hasFinancials(r) && r.Sector != "Financials"
}

// lowDebt says whether debt-to-equity (stored × 100) is under limit. A
// negative one means debts are larger than assets, and a 0 with debt is a
// missing figure.
func lowDebt(r screenRow, limit int64) bool {
	if !balanceSheetFits(r) {
		return false
	}
	if r.TotalDebt == 0 {
		return r.DebtToEquity == 0
	}
	return r.DebtToEquity > 0 && r.DebtToEquity < limit
}

// healthScore gives a point each for a profit, positive free cash flow,
// debt-to-equity under 1, a current ratio over 1 and revenue up on a year
// earlier. Stocks without a balance sheet that fits get no score.
func healthScore(r screenRow) (int, bool) {
	if !balanceSheetFits(r) {
		return 0, false
	}
	n := 0
	for _, ok := range []bool{
		r.NetIncome > 0,
		r.FreeCashFlow > 0,
		lowDebt(r, 100),
		r.CurrentRatio > 100,
		r.QuarterlyRevGrowth > 0,
	} {
		if ok {
			n++
		}
	}
	return n, true
}

// fcfYield is free cash flow as basis points of market value.
func fcfYield(r screenRow) int64 {
	if r.MarketCap <= 0 {
		return 0
	}
	return r.FreeCashFlow * 10000 / r.MarketCap
}

// pctBps formats basis points as a percentage to one place: "23.4%".
func pctBps(bp int64) string { return fmt.Sprintf("%.1f%%", float64(bp)/100) }

// growthBps formats a change in basis points with its sign, rounded to a
// whole percentage: "+22%".
func growthBps(bp int64) string { return fmt.Sprintf("%+.0f%%", float64(bp)/100) }

// screenFilter is one chip on the stocks page. The sector menu narrows any
// of them.
type screenFilter struct {
	Key, Label string
	Group      string                    // what the chip lists, for the caption
	Order      string                    // how it's ordered, if the caption should say
	Explain    string                    // what a financial chip's test means, under the caption
	keep       func(screenRow) bool      // nil keeps every row
	less       func(a, b screenRow) bool // nil keeps the rows' A–Z order
	limit      int                       // how many rows it shows; 0 for all
	figure     func(screenRow) string    // shown on each row; nil for none
}

func biggerCap(a, b screenRow) bool { return a.MarketCap > b.MarketCap }

// screenFilters are the chips, in order: lists, then tests of a company's
// financials (from firstFinFilter). Financial labels say what the test is,
// not what to make of it: The Wealth Lab mustn't read as advice.
var screenFilters = []screenFilter{
	{Key: "all", Label: "All A–Z", Group: "Every stock", Order: "A–Z"},
	{
		Key: "picks", Label: "Rob's Picks", Group: "Rob's Picks",
		keep: func(r screenRow) bool { return r.IsPick == 1 },
		less: func(a, b screenRow) bool { return a.PickOrder < b.PickOrder },
	},
	{
		Key: "mag7", Label: "Mag 7", Group: "The Magnificent 7",
		keep: func(r screenRow) bool { return r.Market == "sp500" && mag7[r.Symbol] },
		less: biggerCap,
	},
	{
		Key: "tech", Label: "Tech", Group: "Tech", Order: "biggest first",
		keep: func(r screenRow) bool { return r.Sector == "Information Technology" },
		less: biggerCap,
	},
	{
		Key: "up", Label: "Gainers", Group: "Today's biggest gainers",
		keep:  func(r screenRow) bool { return r.Today.Amount > 0 },
		less:  func(a, b screenRow) bool { return a.TodayPct > b.TodayPct },
		limit: screenTop,
	},
	{
		Key: "down", Label: "Losers", Group: "Today's biggest losers",
		keep:  func(r screenRow) bool { return r.Today.Amount < 0 },
		less:  func(a, b screenRow) bool { return a.TodayPct < b.TodayPct },
		limit: screenTop,
	},
	{Key: "big", Label: "Biggest", Group: "The biggest companies", less: biggerCap, limit: screenBiggest},
	{
		Key: "active", Label: "Most traded", Group: "Most traded today", Order: "by dollar value",
		keep:  func(r screenRow) bool { return r.Turnover > 0 },
		less:  func(a, b screenRow) bool { return a.Turnover > b.Turnover },
		limit: screenTop,
	},
	{
		Key: "aus", Label: "Aussie", Group: "Australian companies listed in the US",
		keep: func(r screenRow) bool { return market.IsAusUS(r.Symbol, r.Market) },
		less: biggerCap,
	},
	{
		Key: "intl", Label: "International", Group: "Overseas companies listed in the US",
		keep: func(r screenRow) bool { return market.IsOtherUS(r.Symbol, r.Market) },
		less: biggerCap,
	},
	{
		Key: "crypto", Label: "Crypto", Group: "Bitcoin funds and crypto companies",
		keep: func(r screenRow) bool { return market.IsCrypto(r.Symbol, r.Market) },
		less: biggerCap,
	},
	{
		Key: "popular", Label: "Players' favourites", Group: "Most held by players in this game",
		keep:  func(r screenRow) bool { return r.Holders > 0 },
		less:  func(a, b screenRow) bool { return a.Holders > b.Holders },
		limit: screenTop,
	},
	{
		Key: "profit", Label: "Making a profit", Group: "Companies making a profit", Order: "biggest profit first",
		Explain: "Net income, the profit left after all costs and tax, was above zero over the last 12 months.",
		keep:    func(r screenRow) bool { return r.NetIncome > 0 },
		less:    func(a, b screenRow) bool { return a.NetIncome > b.NetIncome },
		figure:  func(r screenRow) string { return "Profit margin " + pctBps(r.ProfitMargin) },
	},
	{
		Key: "loss", Label: "Making a loss", Group: "Companies making a loss", Order: "biggest loss first",
		Explain: "Net income, the profit left after all costs and tax, was below zero over the last 12 months.",
		keep:    func(r screenRow) bool { return r.NetIncome < 0 },
		less:    func(a, b screenRow) bool { return a.NetIncome < b.NetIncome },
		figure:  func(r screenRow) string { return "Loss " + bigMoney(-r.NetIncome) },
	},
	{
		Key: "fcf", Label: "Free cash flow positive", Group: "Companies with positive free cash flow", Order: "highest yield first",
		Explain: "Free cash flow is the cash a company's business brings in, less what it spends on equipment and buildings. Its yield is the last 12 months' free cash flow as a share of the company's market value.",
		keep:    func(r screenRow) bool { return r.FreeCashFlow > 0 },
		less:    func(a, b screenRow) bool { return fcfYield(a) > fcfYield(b) },
		figure:  func(r screenRow) string { return "Free cash flow yield " + pctBps(fcfYield(r)) },
	},
	{
		Key: "margin", Label: "Profit margin over 20%", Group: "Companies with a profit margin over 20%", Order: "highest first",
		Explain: "Profit margin is net income as a share of revenue, over the last 12 months.",
		keep:    func(r screenRow) bool { return r.ProfitMargin > 2000 },
		less:    func(a, b screenRow) bool { return a.ProfitMargin > b.ProfitMargin },
		figure:  func(r screenRow) string { return "Profit margin " + pctBps(r.ProfitMargin) },
	},
	{
		// Twelve Data gives some loss-makers a P/E, so a profit is needed too.
		Key: "pe", Label: "P/E under 15", Group: "Companies with a P/E under 15", Order: "lowest first",
		Explain: "P/E is the share price divided by the last 12 months' earnings per share. Only companies making a profit are included.",
		keep:    func(r screenRow) bool { return r.NetIncome > 0 && r.TrailingPe > 0 && r.TrailingPe < 1500 },
		less:    func(a, b screenRow) bool { return a.TrailingPe < b.TrailingPe },
		figure:  func(r screenRow) string { return fmt.Sprintf("P/E %.1f", float64(r.TrailingPe)/100) },
	},
	{
		Key: "peg", Label: "PEG under 1", Group: "Companies with a PEG under 1", Order: "lowest first",
		Explain: "PEG is the P/E divided by the yearly growth in earnings that analysts expect. Only companies making a profit are included.",
		keep:    func(r screenRow) bool { return r.NetIncome > 0 && r.PegRatio > 0 && r.PegRatio < 100 },
		less:    func(a, b screenRow) bool { return a.PegRatio < b.PegRatio },
		figure:  func(r screenRow) string { return fmt.Sprintf("PEG %.2f", float64(r.PegRatio)/100) },
	},
	{
		Key: "revgrowth", Label: "Revenue up 15% or more", Group: "Companies with revenue up 15% or more", Order: "fastest first",
		Explain: "Revenue in the latest quarter, against the same quarter a year earlier.",
		keep:    func(r screenRow) bool { return r.QuarterlyRevGrowth >= 1500 },
		less:    func(a, b screenRow) bool { return a.QuarterlyRevGrowth > b.QuarterlyRevGrowth },
		figure:  func(r screenRow) string { return "Revenue " + growthBps(r.QuarterlyRevGrowth) },
	},
	{
		Key: "earngrowth", Label: "Earnings up on last year", Group: "Companies with earnings up on last year", Order: "fastest first",
		Explain: "Earnings in the latest quarter, against the same quarter a year earlier. Only companies making a profit are included.",
		keep:    func(r screenRow) bool { return r.NetIncome > 0 && r.QuarterlyEarnGrowth > 0 },
		less:    func(a, b screenRow) bool { return a.QuarterlyEarnGrowth > b.QuarterlyEarnGrowth },
		figure:  func(r screenRow) string { return "Earnings " + growthBps(r.QuarterlyEarnGrowth) },
	},
	{
		Key: "lowdebt", Label: "Debt-to-equity under 0.5", Group: "Companies with debt-to-equity under 0.5", Order: "lowest first",
		Explain: "Debt-to-equity is total debt divided by shareholders' equity, what the company owns less what it owes. Banks and insurers are left out, because the measure doesn't suit them.",
		keep:    func(r screenRow) bool { return lowDebt(r, 50) },
		less:    func(a, b screenRow) bool { return a.DebtToEquity < b.DebtToEquity },
		figure: func(r screenRow) string {
			if r.TotalDebt == 0 {
				return "No debt"
			}
			return fmt.Sprintf("Debt-to-equity %.2f", float64(r.DebtToEquity)/100)
		},
	},
	{
		Key: "netcash", Label: "More cash than debt", Group: "Companies with more cash than debt", Order: "most cash first",
		Explain: "Cash and short-term investments are larger than total debt. Banks and insurers are left out, because their cash is mostly customers' money.",
		keep:    func(r screenRow) bool { return balanceSheetFits(r) && r.TotalCash > r.TotalDebt },
		less:    func(a, b screenRow) bool { return a.TotalCash-a.TotalDebt > b.TotalCash-b.TotalDebt },
		figure:  func(r screenRow) string { return "Net cash " + bigMoney(r.TotalCash-r.TotalDebt) },
	},
	{
		Key: "score", Label: "Health score 4 or 5", Group: "Companies with a health score of 4 or 5", Order: "highest first",
		Explain: "One point each for a profit, positive free cash flow, debt-to-equity under 1, a current ratio over 1 (short-term assets cover short-term debts) and revenue up on a year earlier. Banks and insurers don't get a score.",
		keep:    func(r screenRow) bool { return r.HasHealth && r.Health >= 4 },
		less: func(a, b screenRow) bool {
			if a.Health != b.Health {
				return a.Health > b.Health
			}
			return a.MarketCap > b.MarketCap
		},
	},
}

// screenResult is what the stocks page lists.
type screenResult struct {
	Filter  string // the chip's key, or "" for a search
	Caption string
	Explain string // what a financial chip's test means
	Rows    []screenRow
}

// filterKey returns f if it names a chip, otherwise the default.
func filterKey(f string) string {
	for _, sf := range screenFilters {
		if sf.Key == f {
			return f
		}
	}
	return defaultFilter
}

// applyFilter picks and orders the rows for a chip and sector, or for a
// search, which looks through every stock. holders counts players holding
// each stock, for Favourites, and is nil for the other chips. It never
// reorders rows itself.
func applyFilter(rows []screenRow, f, sector, q string, holders map[int64]int64) screenResult {
	if q = strings.TrimSpace(q); q != "" {
		return searchRows(rows, q)
	}
	f = filterKey(f)
	var sf screenFilter
	for _, c := range screenFilters {
		if c.Key == f {
			sf = c
		}
	}
	res := screenResult{Filter: f, Caption: sf.Group, Explain: sf.Explain}
	if sector != "" {
		res.Caption += " in " + sector
	}
	if sf.Order != "" {
		res.Caption += ", " + sf.Order
	}

	var out []screenRow
	for _, r := range rows {
		if sector != "" && r.Sector != sector {
			continue
		}
		r.Holders = holders[r.ID]
		if sf.keep != nil && !sf.keep(r) {
			continue
		}
		if sf.figure != nil {
			r.Figure = sf.figure(r)
		}
		out = append(out, r)
	}
	if sf.less != nil {
		sort.SliceStable(out, func(i, j int) bool { return sf.less(out[i], out[j]) })
	}
	if sf.limit > 0 && len(out) > sf.limit {
		out = out[:sf.limit]
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

// markRows sets Held (from the player's position in this game) and Watching
// (from their watchlist, which every game shares). rows must be what
// applyFilter returned, never the cached rows, which every player shares.
func (h *Handler) markRows(ctx context.Context, participantID, userID int64, rows []screenRow) {
	held := map[int64]int64{}
	if hs, err := h.q.ListHoldings(ctx, participantID); err == nil {
		for _, x := range hs {
			held[x.StockID] = x.Quantity
		}
	}
	watching := map[int64]bool{}
	if ws, err := h.q.ListWatchlist(ctx, userID); err == nil {
		for _, x := range ws {
			watching[x.StockID] = true
		}
	}
	for i := range rows {
		rows[i].Held = held[rows[i].ID]
		rows[i].Watching = watching[rows[i].ID]
	}
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
