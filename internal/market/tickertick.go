package market

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
)

// TickerTick API: free, no auth, 10 requests/minute rate limit.
// Docs: https://github.com/hczhu/TickerTick-API

const (
	tickertickBaseURL     = "https://api.tickertick.com/feed"
	tickertickInterval    = 10 * time.Second // 6 req/min, well under 10/min limit
	tickertickBackoff     = 65 * time.Second // wait after a 429 before retrying
	tickertickMaxRetries  = 2                // retry attempts per stock on 429
	tickertickNewsLimit   = 20               // max stories per ticker
	tickertickSkipAge     = 4 * time.Hour    // skip stocks fetched within this window
	tickertickHTTPTimeout = 15 * time.Second
)

// TickerTickMarket is the only market to fetch news for. TickerTick's tt:
// tickers are US listings, so ASX codes either match nothing or match an
// unrelated US company (ASX ALL returns Allstate, WES returns Western
// Midstream).
const TickerTickMarket = "sp500"

type tickertickResponse struct {
	Stories []tickertickStory `json:"stories"`
}

type tickertickStory struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Site        string `json:"site"`
	Time        int64  `json:"time"` // milliseconds since epoch
	Description string `json:"description"`
}

// FetchTickerTickNews fetches news from the TickerTick API for all stocks in
// the given market ("sp500", "asx", or "both"). Each stock gets its own API
// call with a 10-second pause between calls to stay well under the 10 req/min
// limit. On a 429, it backs off for 65 seconds and retries.
// Results are cached in the stock_news table, keeping 20 stories per ticker.
func (f *Fetcher) FetchTickerTickNews(ctx context.Context, q *db.Queries, market string) error {
	stocks, err := q.ListAllStocksByMarket(ctx, db.ListAllStocksByMarketParams{
		Market: market, Column2: market,
	})
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: tickertickHTTPTimeout}
	runStart := time.Now()
	newsOK, skipped, errCount := 0, 0, 0

	slog.Info("tickertick news refresh starting", "market", market, "stocks", len(stocks))

	cutoff := time.Now().UTC().Add(-tickertickSkipAge)

	for i, s := range stocks {
		// Skip stocks that already have recent news (e.g. from a prior
		// interrupted run). This lets a restart effectively resume.
		if age, err := q.GetStockNewsAge(ctx, s.ID); err == nil && age != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", age); err == nil && t.After(cutoff) {
				skipped++
				continue
			}
		}

		// Rate limit: wait between calls (skip the first API call).
		if newsOK+errCount > 0 {
			if err := tickertickSleep(ctx, tickertickInterval); err != nil {
				return err
			}
		}

		stories, err := fetchTickerTickWithRetry(ctx, client, s.Symbol)
		if err != nil {
			slog.Warn("tickertick fetch", "symbol", s.Symbol, "error", err)
			errCount++
			continue
		}
		if len(stories) == 0 {
			skipped++
			continue
		}

		// Replace existing news for this stock.
		if err := q.DeleteStockNews(ctx, s.ID); err != nil {
			slog.Error("tickertick news delete", "symbol", s.Symbol, "error", err)
			errCount++
			continue
		}

		limit := tickertickNewsLimit
		if len(stories) < limit {
			limit = len(stories)
		}
		for _, st := range stories[:limit] {
			if st.Title == "" || st.URL == "" {
				continue
			}
			if err := q.InsertStockNews(ctx, db.InsertStockNewsParams{
				StockID:     s.ID,
				Headline:    st.Title,
				Summary:     st.Description,
				Source:      st.Site,
				Url:         st.URL,
				PublishedAt: st.Time / 1000, // ms → unix seconds
			}); err != nil {
				slog.Error("tickertick news insert", "symbol", s.Symbol, "error", err)
			}
		}
		newsOK++

		if (i+1)%25 == 0 {
			slog.Info("tickertick news progress",
				"done", fmt.Sprintf("%d/%d", i+1, len(stocks)),
				"news", newsOK, "skipped", skipped, "errors", errCount,
			)
		}
	}

	slog.Info("tickertick news refresh complete",
		"market", market,
		"stocks", len(stocks),
		"news", newsOK,
		"skipped", skipped,
		"errors", errCount,
		"duration_ms", time.Since(runStart).Milliseconds(),
	)
	return nil
}

// fetchTickerTickWithRetry wraps fetchTickerTickStories with retry logic for
// 429 rate-limit responses. On a 429 it waits 65 seconds (the API blocks for
// 1 minute) and retries up to tickertickMaxRetries times.
func fetchTickerTickWithRetry(ctx context.Context, client *http.Client, symbol string) ([]tickertickStory, error) {
	for attempt := range tickertickMaxRetries + 1 {
		stories, err := fetchTickerTickStories(ctx, client, symbol)
		if err == nil {
			return stories, nil
		}
		if !isRateLimited(err) || attempt == tickertickMaxRetries {
			return nil, err
		}
		slog.Warn("tickertick rate limited, backing off", "symbol", symbol, "wait", tickertickBackoff)
		if err := tickertickSleep(ctx, tickertickBackoff); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("unreachable")
}

// fetchTickerTickStories calls the TickerTick /feed endpoint for a single
// ticker symbol and returns up to tickertickNewsLimit stories.
func fetchTickerTickStories(ctx context.Context, client *http.Client, symbol string) ([]tickertickStory, error) {
	query := fmt.Sprintf("tt:%s", strings.ToLower(symbol))
	u := fmt.Sprintf("%s?q=%s&n=%d", tickertickBaseURL, url.QueryEscape(query), tickertickNewsLimit)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == 1015 {
		return nil, fmt.Errorf("rate limited (%d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}

	var result tickertickResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Stories, nil
}

// tickertickSleep pauses for the given duration, cancellable via ctx.
func tickertickSleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// isRateLimited checks if an error is a TickerTick 429 response.
func isRateLimited(err error) bool {
	return err != nil && strings.Contains(err.Error(), "rate limited")
}
