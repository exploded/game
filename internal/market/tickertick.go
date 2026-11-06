package market

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
)

// TickerTick API: free, no auth, 10 requests/minute rate limit.
// Docs: https://github.com/hczhu/TickerTick-API

// tickertickBaseURL is a var so tests can point it at a stub.
var tickertickBaseURL = "https://api.tickertick.com/feed"

const (
	tickertickInterval    = 10 * time.Second // 6 req/min, well under 10/min limit
	tickertickBackoff     = 65 * time.Second // wait after a 429 before retrying
	tickertickMaxRetries  = 2                // retry attempts per stock on 429
	tickertickFetchLimit  = 50               // stories asked for per ticker; most are filtered out
	tickertickKeepRows    = 60               // stories kept per stock, shown or not
	tickertickSkipAge     = 4 * time.Hour    // skip stocks fetched within this window
	tickertickHTTPTimeout = 15 * time.Second
	newsFilterMaxFails    = 3 // filter failures in a row that end the run
)

// TickerTickMarket is the only market to fetch news for. TickerTick's tt:
// tickers are US listings, so ASX codes either match nothing or match an
// unrelated US company (ASX ALL returns Allstate, WES returns Western
// Midstream).
const TickerTickMarket = "sp500"

// newsSites are the only sites whose stories are stored: free to read and
// worth reading. A story's site matches its own entry or a parent domain's
// (finance.yahoo.com matches yahoo.com).
//
// Left off on purpose (checked 2 November 2026):
//   - Paywalls or sign-in walls: Bloomberg, WSJ, FT, The Information,
//     Barron's, NYT, The Economist, Washington Post, Business Insider,
//     MarketWatch, Seeking Alpha, Reuters, Forbes, IBD.
//   - Filler: marketbeat.com and tickerreport.com (fund holdings filings),
//     theinventory.com and cnet.com (shopping deals), Reddit.
//   - techmeme.com, an aggregator whose links lead on to paywalled sources.
var newsSites = map[string]bool{
	// General news.
	"apnews.com": true, "bbc.com": true, "bbc.co.uk": true, "theguardian.com": true,
	"abc.net.au": true, "npr.org": true, "axios.com": true, "cnbc.com": true,
	// Markets.
	"yahoo.com": true, "fool.com": true, "fool.com.au": true, "investopedia.com": true,
	"barchart.com": true, "benzinga.com": true,
	// Tech and industry.
	"techcrunch.com": true, "theverge.com": true, "arstechnica.com": true,
	"theregister.com": true, "geekwire.com": true, "siliconangle.com": true,
	"qz.com": true, "pymnts.com": true, "datacenterdynamics.com": true,
	"fiercepharma.com": true, "fiercebiotech.com": true,
	// Companies' own press releases.
	"prnewswire.com": true, "businesswire.com": true, "globenewswire.com": true,
}

// newsSiteAllowed says whether site, or a parent domain of it, is in
// newsSites.
func newsSiteAllowed(site string) bool {
	site = strings.ToLower(strings.TrimSpace(site))
	for site != "" {
		if newsSites[site] {
			return true
		}
		_, rest, ok := strings.Cut(site, ".")
		if !ok {
			return false
		}
		site = rest
	}
	return false
}

// NewsStory is a story offered to a NewsFilter.
type NewsStory struct {
	Site     string
	Headline string
	Summary  string
}

// NewsFilter picks the stories worth showing on a company's stock page.
// Keep returns one answer per story, in order.
type NewsFilter interface {
	Keep(ctx context.Context, company, symbol string, stories []NewsStory) ([]bool, error)
}

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
//
// Only stories from newsSites that aren't stored yet go to filter, and each
// is stored with whether filter kept it, so it's never asked twice. With a
// nil filter every one is kept. If filter fails, nothing new is stored for
// that stock, so its stories are offered again on the next run.
func (f *Fetcher) FetchTickerTickNews(ctx context.Context, q *db.Queries, market string, filter NewsFilter) error {
	stocks, err := q.ListAllStocksByMarket(ctx, db.ListAllStocksByMarketParams{
		Market: market, Column2: market,
	})
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: tickertickHTTPTimeout}
	runStart := time.Now()
	newsOK, skipped, errCount, stored, shown := 0, 0, 0, 0, 0

	slog.Info("tickertick news refresh starting", "market", market, "stocks", len(stocks))

	cutoff := time.Now().UTC().Add(-tickertickSkipAge)
	fetched, filterFails := 0, 0

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
		if fetched > 0 {
			if err := tickertickSleep(ctx, tickertickInterval); err != nil {
				return err
			}
		}
		fetched++

		stories, err := fetchTickerTickWithRetry(ctx, client, s.Symbol)
		if err != nil {
			slog.Warn("tickertick fetch", "symbol", s.Symbol, "error", err)
			errCount++
			continue
		}

		known, err := q.ListStockNewsKeys(ctx, s.ID)
		if err != nil {
			slog.Error("tickertick news keys", "symbol", s.Symbol, "error", err)
			errCount++
			continue
		}
		fresh := newStories(stories, known)
		if len(fresh) == 0 {
			skipped++
			continue
		}

		keep := make([]bool, len(fresh))
		for j := range keep {
			keep[j] = true
		}
		if filter != nil {
			offered := make([]NewsStory, len(fresh))
			for j, st := range fresh {
				offered[j] = NewsStory{Site: st.Site, Headline: st.Title, Summary: st.Description}
			}
			keep, err = filter.Keep(ctx, s.Name, s.Symbol, offered)
			if err != nil {
				// A bad key or an outage fails every stock: stop rather
				// than log each one.
				if filterFails++; filterFails == newsFilterMaxFails {
					return fmt.Errorf("news filter failed %d times in a row: %w", filterFails, err)
				}
				slog.Warn("news filter", "symbol", s.Symbol, "error", err)
				errCount++
				continue
			}
			if len(keep) != len(fresh) {
				slog.Error("news filter", "symbol", s.Symbol, "error",
					fmt.Sprintf("%d answers for %d stories", len(keep), len(fresh)))
				errCount++
				continue
			}
			filterFails = 0
		}

		for j, st := range fresh {
			var show int64
			if keep[j] {
				show = 1
				shown++
			}
			if err := q.InsertStockNews(ctx, db.InsertStockNewsParams{
				StockID:     s.ID,
				Headline:    st.Title,
				Summary:     st.Description,
				Source:      st.Site,
				Url:         st.URL,
				PublishedAt: st.Time / 1000, // ms → unix seconds
				Shown:       show,
			}); err != nil {
				slog.Error("tickertick news insert", "symbol", s.Symbol, "error", err)
				continue
			}
			stored++
		}
		if err := q.PruneStockNews(ctx, db.PruneStockNewsParams{StockID: s.ID, Keep: tickertickKeepRows}); err != nil {
			slog.Error("tickertick news prune", "symbol", s.Symbol, "error", err)
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
		"stored", stored,
		"shown", shown,
		"skipped", skipped,
		"errors", errCount,
		"duration_ms", time.Since(runStart).Milliseconds(),
	)
	return nil
}

// newStories returns the stories from newsSites that aren't in known, each
// once, with tracking tags taken off their URLs and their summaries cleaned.
// A story is a repeat if its URL matches without tracking tags, or its site
// has already sent the same headline: fool.com sent one CAH transcript twice
// on 5 November 2026, the second with ?source=iedfolrf0000001.
func newStories(stories []tickertickStory, known []db.ListStockNewsKeysRow) []tickertickStory {
	seen := make(map[string]bool, 2*(len(known)+len(stories)))
	for _, k := range known {
		seen[canonicalNewsURL(k.Url)] = true
		seen[headlineKey(k.Source, k.Headline)] = true
	}
	var fresh []tickertickStory
	for _, st := range stories {
		if st.Title == "" || st.URL == "" || !newsSiteAllowed(st.Site) {
			continue
		}
		st.URL = canonicalNewsURL(st.URL)
		st.Title = strings.Join(strings.Fields(st.Title), " ")
		hk := headlineKey(st.Site, st.Title)
		if seen[st.URL] || seen[hk] {
			continue
		}
		seen[st.URL], seen[hk] = true, true
		st.Description = cleanSummary(st.Description)
		fresh = append(fresh, st)
	}
	return fresh
}

// headlineKey identifies a headline from a site, ignoring case and spacing.
func headlineKey(site, headline string) string {
	return "h:" + strings.ToLower(strings.TrimSpace(site)) + "|" +
		strings.ToLower(strings.Join(strings.Fields(headline), " "))
}

// trackingParams are query parameters that say where a reader came from, not
// which story it is.
var trackingParams = map[string]bool{
	"source": true, "src": true, "ref": true, "referrer": true, "cmpid": true,
	"ncid": true, "fbclid": true, "gclid": true, "mc_cid": true, "mc_eid": true,
	"guccounter": true, "guce_referrer": true, "guce_referrer_sig": true,
}

// canonicalNewsURL drops the fragment and tracking parameters (utm_* and
// trackingParams) from a story's URL. A URL it can't parse is returned as is.
func canonicalNewsURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return raw
	}
	u.Fragment, u.RawFragment = "", ""
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			if trackingParams[strings.ToLower(k)] || strings.HasPrefix(strings.ToLower(k), "utm_") {
				q.Del(k)
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

var (
	// "The post <title> appeared first on <site>." from WordPress feeds.
	appearedFirst = regexp.MustCompile(`(?i)\s*The post .*? appeared first on .*$`)
	// Links and cut-off marks from the end of an RSS summary. They aren't
	// links once stored.
	summaryTails = regexp.MustCompile(`(?i)\s*(continue reading|read more|read the full (article|story)|\[link\]|\[comments\])\s*(\.*|…)\s*$`)
	summaryCut   = regexp.MustCompile(`\s*(\[(…|\.\.\.)\]|\[\s*&hellip;\s*\])\s*$`)
)

// cleanSummary strips the ends RSS feeds add to a summary ("Continue
// reading", "[…]", "The post … appeared first on …") and collapses spaces. A
// summary that was cut short ends with an ellipsis.
func cleanSummary(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	s = appearedFirst.ReplaceAllString(s, "")
	for {
		t := summaryTails.ReplaceAllString(s, "")
		cut := summaryCut.ReplaceAllString(t, "")
		if cut != t {
			t = strings.TrimRight(cut, " .,;:") + "…"
		}
		if t == s {
			return s
		}
		s = t
	}
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
// ticker symbol and returns up to tickertickFetchLimit stories. z: asks for
// stories mainly about the company; tt: also matches any story that mentions
// it, such as shopping deals "on Amazon".
func fetchTickerTickStories(ctx context.Context, client *http.Client, symbol string) ([]tickertickStory, error) {
	query := fmt.Sprintf("z:%s", strings.ToLower(symbol))
	u := fmt.Sprintf("%s?q=%s&n=%d", tickertickBaseURL, url.QueryEscape(query), tickertickFetchLimit)

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
