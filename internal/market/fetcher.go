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
	rawDB  *sql.DB // for transactions around bulk price writes
	client *http.Client
}

func NewFetcher(q *db.Queries, rawDB *sql.DB) *Fetcher {
	return &Fetcher{
		q:     q,
		rawDB: rawDB,
		client: &http.Client{
			Timeout:   60 * time.Second,
			Transport: keyTransport{key: os.Getenv("TWELVEDATA_API_KEY"), next: http.DefaultTransport},
		},
	}
}

// keyTransport sends the Twelve Data key in the Authorization header rather
// than the URL, because a failed request's error quotes the URL and ends up
// in the logs. Only api.twelvedata.com gets it.
type keyTransport struct {
	key  string
	next http.RoundTripper
}

func (t keyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host == "api.twelvedata.com" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "apikey "+t.key)
	}
	return t.next.RoundTrip(r)
}

type twelveDataResponse struct {
	Meta   map[string]any   `json:"meta"`
	Values []map[string]any `json:"values"`
	Status string           `json:"status"`
	Code   int              `json:"code"`
}

// Backfill fetches the full history (outputsize=5000) of every S&P 500
// stock that hasn't had it yet: on the first run, for a new stock, or after
// an interrupted backfill. It does nothing once every stock has its history,
// so it's safe to run at every start.
func (f *Fetcher) Backfill(ctx context.Context, q *db.Queries) error {
	missing, err := q.ListStocksWithoutHistory(ctx, "sp500")
	if err != nil {
		return err
	}
	missing = LimitStocks(missing)
	if len(missing) == 0 {
		return nil
	}
	slog.Info("backfilling stocks without price history", "count", len(missing))
	return f.fetchStocksBatch(ctx, q, "sp500", missing, 5000)
}

// LimitStocks keeps only the stocks named in TWELVEDATA_SYMBOLS (comma
// separated, such as "MSFT,AMZN,BRK.B,IBM"), or returns stocks unchanged if
// it's unset. Every Twelve Data job filters its stock list through here. Set
// it on a dev machine: dev and production share the API key and its 610
// credits a minute, so a dev server tracking every stock would push both over.
func LimitStocks(stocks []db.Stock) []db.Stock {
	only := limitSymbols()
	if only == nil {
		return stocks
	}
	var out []db.Stock
	for _, s := range stocks {
		if only[s.Symbol] {
			out = append(out, s)
		}
	}
	return out
}

// limitSymbols parses TWELVEDATA_SYMBOLS, returning nil when it's unset.
func limitSymbols() map[string]bool {
	v := strings.TrimSpace(os.Getenv("TWELVEDATA_SYMBOLS"))
	if v == "" {
		return nil
	}
	only := make(map[string]bool)
	for _, s := range strings.Split(v, ",") {
		if s = strings.ToUpper(strings.TrimSpace(s)); s != "" {
			only[s] = true
		}
	}
	return only
}

// fetchMarketBatch fetches prices for every stock in a market. Used for daily
// updates. For resumable backfill use fetchStocksBatch directly
// with a pre-filtered list of stocks from ListStocksWithoutHistory.
func (f *Fetcher) fetchMarketBatch(ctx context.Context, q *db.Queries, market string, outputSize int) error {
	stocks, err := q.ListAllStocksByMarket(ctx, db.ListAllStocksByMarketParams{
		Market: market, Column2: market,
	})
	if err != nil {
		return err
	}
	return f.fetchStocksBatch(ctx, q, market, LimitStocks(stocks), outputSize)
}

// Symbols per time_series request. Twelve Data accepts up to 120 symbols in one
// batch request at 1 credit each. Backfill batches are smaller because each
// symbol returns up to 5,000 rows, and a 120-symbol response would be huge.
const (
	dailyBatchSize    = 120
	backfillBatchSize = 20
)

// fetchStocksBatch fetches prices for the given list of stocks in batches.
// Each batch waits for credits from takeCredits, so a pause only happens when
// the minute's budget is spent.
//
// It writes one price_fetch_log row for the run when it ends, however it
// ends. The scheduler reads the last daily row to know whether today's
// daily fetch is done; a backfill's interruption needs no record, because
// ListStocksWithoutHistory finds what's left.
func (f *Fetcher) fetchStocksBatch(ctx context.Context, q *db.Queries, market string, stocks []db.Stock, outputSize int) (runErr error) {
	if len(stocks) == 0 {
		return nil
	}

	runStart := time.Now()
	updated := 0
	symbolsOK := 0
	symbolsSkipped := 0
	var lastErr error

	mode := "daily"
	batchSize := dailyBatchSize
	if outputSize > 1 {
		mode = "backfill"
		batchSize = backfillBatchSize
	}
	defer func() {
		writeFetchLog(context.WithoutCancel(ctx), q, fetchRun{
			market: market, mode: mode, err: runErr,
			rows: updated, ok: symbolsOK, skipped: symbolsSkipped,
			duration: time.Since(runStart),
		})
	}()
	totalBatches := (len(stocks) + batchSize - 1) / batchSize
	slog.Info("price fetch starting",
		"market", market,
		"mode", mode,
		"stocks", len(stocks),
		"batches", totalBatches,
		"batch_size", batchSize,
		"outputsize", outputSize,
	)

	for i := 0; i < len(stocks); i += batchSize {
		end := i + batchSize
		if end > len(stocks) {
			end = len(stocks)
		}
		batch := stocks[i:end]
		batchNum := i/batchSize + 1

		symbols := make([]string, len(batch))
		for j, s := range batch {
			symbols[j] = s.Symbol
		}
		slog.Info("price batch start",
			"market", market,
			"batch", fmt.Sprintf("%d/%d", batchNum, totalBatches),
			"symbols", strings.Join(symbols, ","),
		)

		if err := takeCredits(ctx, len(batch)); err != nil {
			slog.Warn("price fetch cancelled", "market", market, "completed", i, "total", len(stocks))
			return err
		}

		batchStart := time.Now()
		batchUpdated, ok, skipped, err := f.fetchOneBatch(ctx, q, batch, outputSize)
		if isOutOfCredits(err) {
			// Something else on the same key (such as a dev server) used the
			// quota. Wait out the window and try once more.
			slog.Warn("price batch out of credits, retrying",
				"market", market, "batch", fmt.Sprintf("%d/%d", batchNum, totalBatches))
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(creditWindow):
			}
			if err := takeCredits(ctx, len(batch)); err != nil {
				return err
			}
			batchUpdated, ok, skipped, err = f.fetchOneBatch(ctx, q, batch, outputSize)
		}
		updated += batchUpdated
		symbolsOK += ok
		symbolsSkipped += skipped
		batchMs := time.Since(batchStart).Milliseconds()

		if err != nil {
			lastErr = err
			slog.Error("price batch failed",
				"market", market,
				"batch", fmt.Sprintf("%d/%d", batchNum, totalBatches),
				"duration_ms", batchMs,
				"error", err,
			)
		} else {
			slog.Info("price batch done",
				"market", market,
				"batch", fmt.Sprintf("%d/%d", batchNum, totalBatches),
				"rows_saved", batchUpdated,
				"symbols_ok", ok,
				"symbols_skipped", skipped,
				"duration_ms", batchMs,
				"total_rows_so_far", updated,
			)
		}
	}

	slog.Info("price fetch complete",
		"market", market,
		"mode", mode,
		"stocks", len(stocks),
		"rows_saved", updated,
		"symbols_ok", symbolsOK,
		"symbols_skipped", symbolsSkipped,
		"duration_ms", time.Since(runStart).Milliseconds(),
	)
	return lastErr
}

// fetchOneBatch runs a single Twelve Data request for a batch of symbols,
// saves any returned rows, and returns (rowsSaved, symbolsOK, symbolsSkipped, error).
// A non-nil error means the request or decode failed entirely; per-symbol API
// errors inside the response body are logged and counted as skipped but do not
// produce an error.
func (f *Fetcher) fetchOneBatch(ctx context.Context, q *db.Queries, batch []db.Stock, outputSize int) (int, int, int, error) {
	symbols := make([]string, len(batch))
	symbolToStock := make(map[string]db.Stock)
	for j, s := range batch {
		symbols[j] = s.Symbol
		symbolToStock[s.Symbol] = s
	}

	url := fmt.Sprintf("https://api.twelvedata.com/time_series?symbol=%s&interval=1day&outputsize=%d",
		strings.Join(symbols, ","), outputSize)

	resp, err := f.client.Get(url)
	if err != nil {
		return 0, 0, 0, err
	}
	defer resp.Body.Close()

	var body map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, 0, 0, err
	}
	if _, isSingle := body["meta"]; !isSingle {
		if err := requestError(body); err != nil {
			return 0, 0, 0, err
		}
	}

	// A one-symbol request returns the series itself, not a map of series.
	results := make(map[string]twelveDataResponse, len(batch))
	if _, isSingle := body["meta"]; isSingle {
		var td twelveDataResponse
		raw, _ := json.Marshal(body)
		json.Unmarshal(raw, &td)
		results[symbols[0]] = td
	} else {
		for sym, raw := range body {
			var td twelveDataResponse
			json.Unmarshal(raw, &td)
			results[sym] = td
		}
	}

	skipped := 0
	var attempted []db.Stock
	var groups [][]priceRow
	for sym, td := range results {
		s, found := symbolToStock[sym]
		if !found {
			continue
		}
		if td.Status == "error" || len(td.Values) == 0 {
			slog.Warn("price symbol skipped", "symbol", sym, "status", td.Status, "code", td.Code, "values", len(td.Values))
			skipped++
			continue
		}
		rows := make([]priceRow, len(td.Values))
		for i, v := range td.Values {
			rows[i] = priceRow{stockID: s.ID, v: v}
		}
		attempted = append(attempted, s)
		groups = append(groups, rows)
	}

	// One transaction per stock for a backfill (up to 5,000 rows each, which
	// keeps each write lock short), one for the whole batch for a daily update.
	if outputSize == 1 {
		var all []priceRow
		for _, g := range groups {
			all = append(all, g...)
		}
		groups = [][]priceRow{all}
	}
	saved := make(map[int64]int)
	for _, g := range groups {
		n, err := f.saveRows(ctx, q, g)
		if err != nil {
			return 0, 0, skipped, err
		}
		for id, c := range n {
			saved[id] += c
		}
	}

	updated, ok := 0, 0
	for _, s := range attempted {
		if rows := saved[s.ID]; rows > 0 {
			updated += rows
			ok++
			slog.Debug("price symbol saved", "symbol", s.Symbol, "rows", rows)
			if outputSize > 1 {
				// The whole history is in, however short, so the backfill
				// can leave this stock alone (ListStocksWithoutHistory).
				if err := q.MarkHistoryFetched(ctx, s.ID); err != nil {
					slog.Warn("marking history fetched", "symbol", s.Symbol, "error", err)
				}
			}
		} else {
			skipped++
			slog.Warn("price symbol empty", "symbol", s.Symbol)
		}
	}
	return updated, ok, skipped, nil
}

// priceRow is one bar for a stock, as decoded from Twelve Data's JSON.
type priceRow struct {
	stockID int64
	v       map[string]any
}

// saveRows upserts rows in a single transaction and returns how many were
// saved per stock. Without a transaction SQLite commits (and syncs the WAL)
// on every row, which makes a 5,000-row backfill crawl and holds up web
// requests on the single connection.
func (f *Fetcher) saveRows(ctx context.Context, q *db.Queries, rows []priceRow) (map[int64]int, error) {
	saved := make(map[int64]int)
	if len(rows) == 0 {
		return saved, nil
	}
	tx, err := f.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return saved, err
	}
	defer tx.Rollback()

	txq := q.WithTx(tx)
	for _, r := range rows {
		if savePrice(ctx, txq, r.stockID, r.v) {
			saved[r.stockID]++
		}
	}
	if err := tx.Commit(); err != nil {
		return make(map[int64]int), err
	}
	return saved, nil
}

// requestError returns an error if a batch response body is a whole-request
// error, such as {"code":429,"message":"...","status":"error"}, rather than a
// map of symbol to result.
func requestError(body map[string]json.RawMessage) error {
	if _, ok := body["code"]; !ok {
		return nil
	}
	var e struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	raw, _ := json.Marshal(body)
	json.Unmarshal(raw, &e)
	return fmt.Errorf("twelve data error %d: %s", e.Code, e.Message)
}

// isOutOfCredits reports whether err is Twelve Data's per-minute credit limit.
func isOutOfCredits(err error) bool {
	return err != nil && strings.Contains(err.Error(), "twelve data error 429")
}

// fetchRun is what a daily fetch or backfill did, for price_fetch_log.
type fetchRun struct {
	market, mode      string
	err               error // the last error, if any
	rows, ok, skipped int   // rows saved, symbols saved, symbols skipped
	duration          time.Duration
}

// status is "success" when every symbol was saved, "partial" when some
// were, and "error" when none were.
func (r fetchRun) status() string {
	switch {
	case r.err == nil && r.skipped == 0:
		return "success"
	case r.ok > 0:
		return "partial"
	}
	return "error"
}

func writeFetchLog(ctx context.Context, q *db.Queries, r fetchRun) {
	var errMsg *string
	if r.err != nil {
		s := r.err.Error()
		errMsg = &s
	}
	if err := q.InsertFetchLog(ctx, db.InsertFetchLogParams{
		Market:         r.market,
		Mode:           r.mode,
		Status:         r.status(),
		StocksUpdated:  int64(r.rows),
		SymbolsOk:      int64(r.ok),
		SymbolsSkipped: int64(r.skipped),
		ErrorMsg:       nullString(errMsg),
		DurationMs:     nullInt64(r.duration.Milliseconds()),
	}); err != nil {
		slog.Error("writing price fetch log", "error", err)
	}
}

// FetchMarketOnly fetches the latest prices for a single market (daily update).
func (f *Fetcher) FetchMarketOnly(q *db.Queries, mkt string) {
	ctx := context.Background()
	f.fetchMarketBatch(ctx, q, mkt, 1)
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
