package market

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
)

// New listings. Twelve Data's /ipo_calendar can't be relied on: on
// 19 November 2026 it ignored its date range and left out Anthropic, due to
// list on the Nasdaq the next day. Its /stocks list had Anthropic already,
// so CheckListings loads the NYSE and Nasdaq lists each weekday and treats a
// symbol that wasn't there before as a new listing. Once one trades, a
// /statistics call gives its market cap, and admins are emailed about any
// at S&P 500 size. Admins add the ones they want at /admin/stocks.

// SPSizeCap is the market cap, in whole US dollars, from which a new listing
// counts as S&P 500 size and admins are told about it. S&P's minimum for
// joining the index was US$22.7 billion from July 2025; this sits a little
// under it so a listing near the line isn't missed. Change it if S&P moves
// its minimum a long way.
const SPSizeCap = 20_000_000_000

// listingsCost is what one exchange's /stocks list costs. Twelve Data sent
// no credits header for it on 19 November 2026; 1 is a guess on the safe side.
const listingsCost = 1

// listingExchanges are the exchanges checked, each with the fewest stocks a
// whole list can have. A short list is treated as a failed request: saving
// it as the baseline would make the missing stocks look new the next day.
// Nasdaq had 4,517 entries on 19 November 2026.
var listingExchanges = []struct {
	name string
	min  int
}{{"NASDAQ", 1500}, {"NYSE", 500}}

// listingTypes are the kinds of entry that can be a new company. Warrants,
// units, preferred shares and notes aren't.
var listingTypes = map[string]bool{
	"Common Stock":                true,
	"American Depositary Receipt": true,
	"Depositary Receipt":          true,
}

// listingMaxChecks is how many weekdays in a row a new listing is looked up
// before giving up: /statistics has nothing until a stock has traded, and a
// listing can appear in the list a week or two before its first trade.
const listingMaxChecks = 15

// listingWindow is how long after first appearing a listing is still new.
const listingWindow = 30 * 24 * time.Hour

// tdListing is one entry in Twelve Data's /stocks list.
type tdListing struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Exchange string `json:"exchange"`
	Type     string `json:"type"`
	Country  string `json:"country"`
}

// NewListing is a new listing admins should hear about.
type NewListing struct {
	Symbol, Exchange, Name string
	MarketCap              int64 // whole US dollars
}

// CheckListings loads the NYSE and Nasdaq lists, saves symbols not seen
// before, looks up the market cap of new ones that don't have one yet, and
// returns the new listings at S&P 500 size that admins haven't been told
// about (marking them told). today is a New York date. The first run saves
// the lists as a baseline, so it finds nothing new.
func (f *Fetcher) CheckListings(ctx context.Context, q *db.Queries, today string) ([]NewListing, error) {
	day, err := time.Parse(time.DateOnly, today)
	if err != nil {
		return nil, err
	}

	// Both lists first, so a failure saves nothing.
	var all []tdListing
	for _, ex := range listingExchanges {
		var resp struct {
			Data []tdListing `json:"data"`
		}
		if err := f.getQuery(ctx, "stocks", listingsCost, url.Values{"exchange": {ex.name}}, &resp); err != nil {
			return nil, fmt.Errorf("%s list: %w", ex.name, err)
		}
		if len(resp.Data) < ex.min {
			return nil, fmt.Errorf("%s list has only %d stocks", ex.name, len(resp.Data))
		}
		all = append(all, resp.Data...)
	}

	known, err := q.CountUSListings(ctx)
	if err != nil {
		return nil, err
	}
	baseline := int64(0)
	if known == 0 {
		baseline = 1
	}
	if err := f.saveListings(ctx, q, all, today, baseline); err != nil {
		return nil, err
	}

	toCheck, err := q.ListListingsToCheck(ctx, db.ListListingsToCheckParams{
		MaxChecks: listingMaxChecks,
		Since:     day.Add(-listingWindow).Format(time.DateOnly),
	})
	if err != nil {
		return nil, err
	}
	for _, l := range toCheck {
		var st tdStatistics
		mcap := int64(0)
		if err := f.getJSON(ctx, "statistics", 50, l.Symbol, &st); err == nil {
			mcap = max(whole(st.Statistics.Valuations.MarketCap), 0)
		} else if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if err := q.SetListingChecked(ctx, db.SetListingCheckedParams{
			MarketCap: mcap, Symbol: l.Symbol, Exchange: l.Exchange,
		}); err != nil {
			return nil, err
		}
	}

	big, err := q.ListListingsToAlert(ctx, SPSizeCap)
	if err != nil {
		return nil, err
	}
	var out []NewListing
	for _, l := range big {
		if err := q.MarkListingAlerted(ctx, db.MarkListingAlertedParams{Symbol: l.Symbol, Exchange: l.Exchange}); err != nil {
			return out, err
		}
		out = append(out, NewListing{Symbol: l.Symbol, Exchange: l.Exchange, Name: l.Name, MarketCap: l.MarketCap})
	}
	slog.Info("new listings checked", "listed", len(all), "baseline", baseline == 1,
		"looked_up", len(toCheck), "sp_size", len(out))
	return out, nil
}

// saveListings inserts the symbols not seen before, in one transaction:
// about 7,000 rows, which would otherwise commit one at a time.
func (f *Fetcher) saveListings(ctx context.Context, q *db.Queries, all []tdListing, today string, baseline int64) error {
	tx, err := f.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	txq := q.WithTx(tx)
	for _, l := range all {
		if !listingTypes[l.Type] || l.Symbol == "" {
			continue
		}
		if err := txq.InsertUSListing(ctx, db.InsertUSListingParams{
			Symbol: l.Symbol, Exchange: l.Exchange, Name: l.Name, Type: l.Type,
			FirstSeen: today, Baseline: baseline,
		}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Sectors are the sectors stocks are filed under: the S&P 500's GICS
// sectors, with "Healthcare" as one word.
var Sectors = []string{
	"Communication Services", "Consumer Discretionary", "Consumer Staples",
	"Energy", "Financials", "Healthcare", "Industrials",
	"Information Technology", "Materials", "Real Estate", "Utilities",
}

// tdSectors maps Twelve Data's sector names, which follow Morningstar's,
// to Sectors.
var tdSectors = map[string]string{
	"technology":             "Information Technology",
	"information technology": "Information Technology",
	"healthcare":             "Healthcare",
	"health care":            "Healthcare",
	"financial services":     "Financials",
	"financial":              "Financials",
	"financials":             "Financials",
	"consumer cyclical":      "Consumer Discretionary",
	"consumer discretionary": "Consumer Discretionary",
	"consumer defensive":     "Consumer Staples",
	"consumer staples":       "Consumer Staples",
	"communication services": "Communication Services",
	"industrials":            "Industrials",
	"energy":                 "Energy",
	"basic materials":        "Materials",
	"materials":              "Materials",
	"real estate":            "Real Estate",
	"utilities":              "Utilities",
}

// SectorFor maps a Twelve Data sector name to one of Sectors, or "".
func SectorFor(td string) string {
	return tdSectors[strings.ToLower(strings.TrimSpace(td))]
}

// ErrNoSuchStock is LookupStock's answer for a symbol Twelve Data has no
// NYSE or Nasdaq stock for, as against a request that failed.
var ErrNoSuchStock = errors.New("no NYSE or Nasdaq stock with that symbol")

// FoundStock is what Twelve Data knows about a symbol, for the add form.
type FoundStock struct {
	Symbol, Name, Exchange, Type string
	Sector                       string // one of Sectors, or "" if Twelve Data has no profile yet
}

// LookupStock finds a symbol's NYSE or Nasdaq listing in Twelve Data's
// /stocks list, and its sector from /profile if there is one yet (a stock
// that hasn't traded has none). It costs about 11 credits.
func (f *Fetcher) LookupStock(ctx context.Context, symbol string) (FoundStock, error) {
	var resp struct {
		Data []tdListing `json:"data"`
	}
	if err := f.getQuery(ctx, "stocks", listingsCost, url.Values{"symbol": {symbol}}, &resp); err != nil {
		return FoundStock{}, err
	}
	var found *tdListing
	for i, l := range resp.Data {
		if l.Exchange == "NYSE" || l.Exchange == "NASDAQ" {
			found = &resp.Data[i]
			break
		}
	}
	if found == nil {
		return FoundStock{}, ErrNoSuchStock
	}
	out := FoundStock{Symbol: found.Symbol, Name: found.Name, Exchange: found.Exchange, Type: found.Type}
	var p tdProfile
	if err := f.getJSON(ctx, "profile", 10, symbol, &p); err == nil {
		out.Sector = SectorFor(p.Sector)
	}
	return out, nil
}

// FetchNewStock fetches the price history and profile of a stock an admin
// just added. A stock that hasn't traded yet gets neither: the quotes, the
// daily fetch and the backfill after it (maybeFetchPrices in the scheduler)
// pick it up once it does, and the nightly profile refresh fills its profile.
func (f *Fetcher) FetchNewStock(ctx context.Context, q *db.Queries, s db.Stock) {
	if err := f.fetchStocksBatch(ctx, q, "sp500", []db.Stock{s}, 5000); err != nil {
		slog.Warn("new stock history", "symbol", s.Symbol, "error", err)
	}
	if err := f.refreshProfile(ctx, q, s); err != nil {
		slog.Warn("new stock profile", "symbol", s.Symbol, "error", err)
	}
}

// Billions writes a market cap in whole dollars as "US$61.5 billion".
func Billions(dollars int64) string {
	b := float64(dollars) / 1e9
	if b >= 100 {
		return fmt.Sprintf("US$%.0f billion", b)
	}
	return fmt.Sprintf("US$%.1f billion", b)
}
