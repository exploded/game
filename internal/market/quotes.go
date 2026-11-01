package market

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/exploded/game/internal/db"
)

// NewYork is the US market's timezone, used for session hours and the quote
// date. Nil if the timezone database is missing, which disables quotes.
var NewYork = loadNewYork()

func loadNewYork() *time.Location {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		slog.Error("load America/New_York, intraday quotes disabled", "error", err)
		return nil
	}
	return loc
}

// quoteBatchSize is the number of symbols per /quote request (1 credit each).
const quoteBatchSize = 120

// FetchQuotes updates today's price row for every S&P 500 stock from Twelve
// Data's /quote endpoint, so trades, limit orders and portfolio values follow
// the live price during the US session. The nightly time_series fetch later
// overwrites each row with the official daily bar.
//
// today is the current date in New York. Quotes dated earlier are skipped:
// outside a session, /quote returns the last session's bar, and saving it would
// overwrite the official close already stored by the nightly fetch. This also
// makes market holidays harmless.
//
// Returns the number of price rows saved.
func (f *Fetcher) FetchQuotes(ctx context.Context, q *db.Queries, today string) (int, error) {
	stocks, err := q.ListAllStocksByMarket(ctx, db.ListAllStocksByMarketParams{
		Market: "sp500", Column2: "sp500",
	})
	if err != nil {
		return 0, err
	}
	stocks = LimitStocks(stocks)

	start := time.Now()
	saved, stale := 0, 0
	var lastErr error
	for i := 0; i < len(stocks); i += quoteBatchSize {
		batch := stocks[i:min(i+quoteBatchSize, len(stocks))]
		if err := takeCredits(ctx, len(batch)); err != nil {
			return saved, err
		}
		s, st, err := f.fetchQuoteBatch(ctx, q, batch, today)
		saved += s
		stale += st
		if err != nil {
			lastErr = err
			slog.Error("quote batch failed", "first_symbol", batch[0].Symbol, "error", err)
		}
	}

	slog.Info("quotes fetched",
		"stocks", len(stocks),
		"rows_saved", saved,
		"stale", stale,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return saved, lastErr
}

// fetchQuoteBatch runs one /quote request and saves quotes dated today.
// Returns (rowsSaved, staleQuotes, error).
func (f *Fetcher) fetchQuoteBatch(ctx context.Context, q *db.Queries, batch []db.Stock, today string) (int, int, error) {
	symbols := make([]string, len(batch))
	bySymbol := make(map[string]db.Stock, len(batch))
	for i, s := range batch {
		symbols[i] = s.Symbol
		bySymbol[s.Symbol] = s
	}

	url := fmt.Sprintf("https://api.twelvedata.com/quote?symbol=%s", strings.Join(symbols, ","))
	resp, err := f.client.Get(url)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, 0, err
	}
	if err := requestError(body); err != nil {
		return 0, 0, err
	}

	// A one-symbol request returns the quote itself, not a map of quotes.
	quotes := body
	if _, isSingle := body["symbol"]; isSingle {
		raw, _ := json.Marshal(body)
		quotes = map[string]json.RawMessage{symbols[0]: raw}
	}

	stale := 0
	var rows []priceRow
	for sym, raw := range quotes {
		s, found := bySymbol[sym]
		if !found {
			continue
		}
		var v map[string]any
		if err := json.Unmarshal(raw, &v); err != nil {
			continue
		}
		if status, _ := v["status"].(string); status == "error" {
			slog.Warn("quote symbol error", "symbol", sym, "message", v["message"])
			continue
		}
		lastAt, _ := v["last_quote_at"].(float64)
		isOpen, _ := v["is_market_open"].(bool)
		noteQuote(time.Unix(int64(lastAt), 0), isOpen)

		date, _ := v["datetime"].(string)
		if len(date) < 10 || date[:10] != today {
			stale++
			continue
		}
		rows = append(rows, priceRow{stockID: s.ID, v: v})
	}

	saved, err := f.saveRows(ctx, q, rows)
	if err != nil {
		return 0, stale, err
	}
	n := 0
	for _, c := range saved {
		n += c
	}
	return n, stale, nil
}

// quoteStatus is what intraday quotes have said about the US session.
var quoteStatus struct {
	mu   sync.Mutex
	at   time.Time // newest last_quote_at seen
	open bool      // is_market_open on that quote
}

func noteQuote(at time.Time, open bool) {
	quoteStatus.mu.Lock()
	defer quoteStatus.mu.Unlock()
	if at.After(quoteStatus.at) {
		quoteStatus.at = at
		quoteStatus.open = open
	}
}

// QuoteStatus returns the time of the newest intraday quote and whether the US
// market is open. Open needs Twelve Data to have said so and the newest quote
// to be under 5 minutes old, so it turns false soon after the close, on
// holidays, and if quotes stop arriving. at is zero until the first quotes
// arrive after a restart.
func QuoteStatus() (at time.Time, open bool) {
	quoteStatus.mu.Lock()
	defer quoteStatus.mu.Unlock()
	return quoteStatus.at, quoteStatus.open && time.Since(quoteStatus.at) < 5*time.Minute
}
