package market

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
)

type Fetcher struct {
	q      *db.Queries
	apiKey string
	client *http.Client
}

func NewFetcher(q *db.Queries) *Fetcher {
	return &Fetcher{
		q:      q,
		apiKey: os.Getenv("TWELVEDATA_API_KEY"),
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type twelveDataResponse struct {
	Meta   map[string]any   `json:"meta"`
	Values []map[string]any `json:"values"`
	Status string           `json:"status"`
	Code   int              `json:"code"`
}

// FetchAll fetches prices for all active stocks and the AUD/USD exchange rate.
func (f *Fetcher) FetchAll(q *db.Queries) error {
	ctx := context.Background()

	// Fetch ASX prices.
	if err := f.fetchMarket(ctx, q, "asx"); err != nil {
		slog.Error("fetch asx prices", "error", err)
	}

	// Fetch S&P 500 prices.
	if err := f.fetchMarket(ctx, q, "sp500"); err != nil {
		slog.Error("fetch sp500 prices", "error", err)
	}

	// Fetch exchange rate.
	if err := f.fetchForex(ctx, q); err != nil {
		slog.Error("fetch forex", "error", err)
	}

	return nil
}

func (f *Fetcher) fetchMarket(ctx context.Context, q *db.Queries, market string) error {
	start := time.Now()

	stocks, err := q.ListStocksByMarket(ctx, db.ListStocksByMarketParams{
		Market: market, Column2: market,
	})
	if err != nil {
		return err
	}
	if len(stocks) == 0 {
		return nil
	}

	// Free tier: 8 API credits/min. Each symbol in a batch = 1 credit.
	// Use batch of 8 symbols, then wait 60s to stay within limits.
	batchSize := 8
	updated := 0
	var lastErr error

	for i := 0; i < len(stocks); i += batchSize {
		end := i + batchSize
		if end > len(stocks) {
			end = len(stocks)
		}
		batch := stocks[i:end]

		// Build comma-separated symbol list.
		symbols := make([]string, len(batch))
		symbolToStock := make(map[string]db.Stock)
		for j, s := range batch {
			apiSymbol := s.Symbol
			if market == "asx" {
				apiSymbol = s.Symbol + ":ASX"
			}
			symbols[j] = apiSymbol
			symbolToStock[apiSymbol] = s
		}

		url := fmt.Sprintf("https://api.twelvedata.com/time_series?symbol=%s&interval=1day&outputsize=1&apikey=%s",
			strings.Join(symbols, ","), f.apiKey)

		resp, err := f.client.Get(url)
		if err != nil {
			lastErr = err
			slog.Error("twelve data request", "error", err)
			continue
		}

		var body map[string]json.RawMessage
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			resp.Body.Close()
			lastErr = err
			continue
		}
		resp.Body.Close()

		// Single symbol: response is the object directly (keys: meta, values, status).
		// Batch: response is keyed by symbol, each value is {meta, values, status}.
		// Detect by checking if "meta" key exists (single) or not (batch).
		if _, isSingle := body["meta"]; isSingle {
			sym := symbols[0]
			var td twelveDataResponse
			raw, _ := json.Marshal(body)
			json.Unmarshal(raw, &td)
			if td.Status == "error" {
				slog.Warn("twelve data error", "symbol", sym, "status", td.Status)
			} else if len(td.Values) > 0 {
				if s, ok := symbolToStock[sym]; ok {
					if savePrice(ctx, q, s.ID, td.Values[0]) {
						updated++
					}
				}
			}
		} else {
			for sym, raw := range body {
				var td twelveDataResponse
				json.Unmarshal(raw, &td)
				if td.Status == "error" || len(td.Values) == 0 {
					slog.Debug("twelve data skip", "symbol", sym, "status", td.Status)
					continue
				}
				if s, ok := symbolToStock[sym]; ok {
					if savePrice(ctx, q, s.ID, td.Values[0]) {
						updated++
					}
				}
			}
		}

		// Free tier: 8 credits/min. Each symbol = 1 credit. Wait 61s between batches.
		if end < len(stocks) {
			slog.Info("rate limit pause", "market", market, "progress", fmt.Sprintf("%d/%d", end, len(stocks)))
			time.Sleep(61 * time.Second)
		}
	}

	duration := time.Since(start).Milliseconds()
	status := "success"
	var errMsg *string
	if lastErr != nil {
		status = "partial"
		s := lastErr.Error()
		errMsg = &s
	}

	q.InsertFetchLog(ctx, db.InsertFetchLogParams{
		Market:        market,
		Status:        status,
		StocksUpdated: int64(updated),
		ErrorMsg:      nullString(errMsg),
		DurationMs:    nullInt64(duration),
	})

	slog.Info("price fetch complete", "market", market, "updated", updated, "duration_ms", duration)
	return lastErr
}

func (f *Fetcher) fetchForex(ctx context.Context, q *db.Queries) error {
	url := fmt.Sprintf("https://api.twelvedata.com/exchange_rate?symbol=AUD/USD&apikey=%s", f.apiKey)
	resp, err := f.client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// exchange_rate endpoint returns: {"symbol":"AUD/USD","rate":"0.63050","timestamp":...}
	var result struct {
		Symbol    string `json:"symbol"`
		Rate      string `json:"rate"`
		Timestamp int64  `json:"timestamp"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}
	if result.Rate == "" {
		return fmt.Errorf("no forex rate returned for AUD/USD")
	}

	closeFloat, err := strconv.ParseFloat(result.Rate, 64)
	if err != nil {
		return fmt.Errorf("invalid forex rate %q: %w", result.Rate, err)
	}

	date := time.Now().UTC().Format("2006-01-02")

	rateInt := int64(math.Round(closeFloat * 1000000))
	q.UpsertExchangeRate(ctx, db.UpsertExchangeRateParams{
		Date:       date,
		RateAudUsd: rateInt,
	})

	q.InsertFetchLog(ctx, db.InsertFetchLogParams{
		Market:        "forex",
		Status:        "success",
		StocksUpdated: 1,
		DurationMs:    nullInt64(0),
	})

	slog.Info("forex rate updated", "date", date, "rate", closeFloat)
	return nil
}

// FetchMarketOnly fetches prices for a single market.
func (f *Fetcher) FetchMarketOnly(q *db.Queries, mkt string) {
	ctx := context.Background()
	f.fetchMarket(ctx, q, mkt)
}

// FetchForexOnly fetches the AUD/USD exchange rate.
func (f *Fetcher) FetchForexOnly(q *db.Queries) {
	ctx := context.Background()
	f.fetchForex(ctx, q)
}

func savePrice(ctx context.Context, q *db.Queries, stockID int64, v map[string]any) bool {
	date, _ := v["datetime"].(string)
	if len(date) > 10 {
		date = date[:10]
	}
	openC := parseCents(v["open"])
	highC := parseCents(v["high"])
	lowC := parseCents(v["low"])
	closeC := parseCents(v["close"])
	vol := parseInt(v["volume"])

	if closeC == 0 {
		return false
	}

	q.UpsertPrice(ctx, db.UpsertPriceParams{
		StockID: stockID,
		Date:    date,
		Open:    openC,
		High:    highC,
		Low:     lowC,
		Close:   closeC,
		Volume:  vol,
	})
	return true
}

func parseCents(v any) int64 {
	switch val := v.(type) {
	case string:
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return 0
		}
		return int64(math.Round(f * 100))
	case float64:
		return int64(math.Round(val * 100))
	}
	return 0
}

func parseInt(v any) int64 {
	switch val := v.(type) {
	case string:
		n, _ := strconv.ParseInt(val, 10, 64)
		return n
	case float64:
		return int64(val)
	}
	return 0
}

func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullInt64(n int64) sql.NullInt64 {
	return sql.NullInt64{Int64: n, Valid: true}
}
