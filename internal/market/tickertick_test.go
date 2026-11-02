package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/exploded/game/internal/db"
)

func TestCleanSummary(t *testing.T) {
	for in, want := range map[string]string{
		// Endings seen from TickerTick on 2 November 2026.
		"Amazon ranks first on major shopping platforms.Continue reading":                               "Amazon ranks first on major shopping platforms.",
		"an additional 50,363 shares during […]":                                                        "an additional 50,363 shares during…",
		"reports of widespread overcharging. Continue reading...":                                       "reports of widespread overcharging.",
		"Investors weigh the quarter [...]":                                                             "Investors weigh the quarter…",
		"Alexa gets agents. The post Amazon Positions Alexa for Shopping appeared first on PYMNTS.com.": "Alexa gets agents.",
		"great phone submitted by /u/someone [link] [comments]":                                         "great phone submitted by /u/someone",
		"  Plain   summary\n with   spaces ":                                                            "Plain summary with spaces",
		"Ends with dots...":                                                                             "Ends with dots...",
		"":                                                                                              "",
	} {
		if got := cleanSummary(in); got != want {
			t.Errorf("cleanSummary(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewsSiteAllowed(t *testing.T) {
	for site, want := range map[string]bool{
		"yahoo.com":         true,
		"finance.yahoo.com": true,
		"FOOL.COM":          true,
		"www.bbc.co.uk":     true,
		"notyahoo.com":      false,
		"bloomberg.com":     false,
		"seekingalpha.com":  false,
		"co.uk":             false,
		"com":               false,
		"":                  false,
	} {
		if got := newsSiteAllowed(site); got != want {
			t.Errorf("newsSiteAllowed(%q) = %v, want %v", site, got, want)
		}
	}
}

// fakeNewsFilter keeps every story except those whose headline is in drop,
// and records what it was offered.
type fakeNewsFilter struct {
	drop    map[string]bool
	err     error
	offered []string
}

func (f *fakeNewsFilter) Keep(_ context.Context, _, _ string, stories []NewsStory) ([]bool, error) {
	keep := make([]bool, len(stories))
	for i, s := range stories {
		f.offered = append(f.offered, s.Headline)
		keep[i] = !f.drop[s.Headline]
	}
	if f.err != nil {
		return nil, f.err
	}
	return keep, nil
}

// TestFetchTickerTickNews: only allowed sites reach the filter, each story
// once; turned-down stories are stored hidden and not offered again; a
// filter error stores nothing, so the stories are offered next time.
func TestFetchTickerTickNews(t *testing.T) {
	d, q := monthlyDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'AMZN', 'Amazon', 'sp500', 'USD')`); err != nil {
		t.Fatal(err)
	}

	var stories []tickertickStory
	minute := int64(0)
	story := func(site, title string) tickertickStory { // each a minute newer
		minute++
		return tickertickStory{Title: title, URL: "https://" + site + "/" + title, Site: site,
			Time: (1_800_000_000 + minute*60) * 1000, Description: title + " summary. Continue reading"}
	}
	stories = []tickertickStory{
		story("pymnts.com", "alexa"),
		story("finance.yahoo.com", "deal"),
		story("bloomberg.com", "paywalled"),
		{Title: "", URL: "https://qz.com/x", Site: "qz.com"},
	}
	stories = append(stories, stories[0]) // the same story twice
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("q"); got != "z:amzn" {
			t.Errorf("query %q, want z:amzn", got)
		}
		json.NewEncoder(w).Encode(tickertickResponse{Stories: stories})
	}))
	defer srv.Close()
	old := tickertickBaseURL
	tickertickBaseURL = srv.URL
	defer func() { tickertickBaseURL = old }()

	f := &Fetcher{q: q, rawDB: d}
	stale := func() { // so the next run doesn't skip the stock as fresh
		d.Exec(`UPDATE stock_news SET created_at = datetime('now', '-5 hours')`)
	}
	shown := func() (rows []string) {
		news, err := q.ListStockNews(ctx, db.ListStockNewsParams{StockID: 1, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range news {
			rows = append(rows, n.Headline)
		}
		return rows
	}

	filter := &fakeNewsFilter{drop: map[string]bool{"deal": true}}
	if err := f.FetchTickerTickNews(ctx, q, "sp500", filter); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(filter.offered); got != "[alexa deal]" {
		t.Errorf("offered %s, want [alexa deal]", got)
	}
	if got := fmt.Sprint(shown()); got != "[alexa]" {
		t.Errorf("shown %s, want [alexa]", got)
	}
	var summary string
	d.QueryRow(`SELECT summary FROM stock_news WHERE headline = 'alexa'`).Scan(&summary)
	if summary != "alexa summary." {
		t.Errorf("summary %q, want it cleaned", summary)
	}

	// A filter error stores nothing new.
	stale()
	stories = append(stories, story("qz.com", "loan"))
	filter = &fakeNewsFilter{err: errors.New("overloaded")}
	if err := f.FetchTickerTickNews(ctx, q, "sp500", filter); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(filter.offered); got != "[loan]" {
		t.Errorf("after the first run, offered %s, want only [loan]", got)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM stock_news`).Scan(&n)
	if n != 2 {
		t.Errorf("%d rows after a filter error, want 2", n)
	}

	// The next run offers it again.
	stale()
	filter = &fakeNewsFilter{}
	if err := f.FetchTickerTickNews(ctx, q, "sp500", filter); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(filter.offered); got != "[loan]" {
		t.Errorf("after the error, offered %s, want [loan]", got)
	}
	if got := fmt.Sprint(shown()); got != "[loan alexa]" {
		t.Errorf("shown %s, want [loan alexa]", got)
	}

	// Without a filter every allowed story shows.
	stale()
	stories = append(stories, story("cnbc.com", "results"))
	if err := f.FetchTickerTickNews(ctx, q, "sp500", nil); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(shown()); got != "[results loan alexa]" {
		t.Errorf("with no filter, shown %s, want [results loan alexa]", got)
	}
}

func TestPruneStockNews(t *testing.T) {
	d, q := monthlyDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'A', 'A', 'sp500', 'USD'), (2, 'B', 'B', 'sp500', 'USD')`); err != nil {
		t.Fatal(err)
	}
	for i := range tickertickKeepRows + 10 {
		for stock := int64(1); stock <= 2; stock++ {
			if err := q.InsertStockNews(ctx, db.InsertStockNewsParams{
				StockID: stock, Headline: "h", Url: fmt.Sprint(i), PublishedAt: int64(i),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := q.PruneStockNews(ctx, db.PruneStockNewsParams{StockID: 1, Keep: tickertickKeepRows}); err != nil {
		t.Fatal(err)
	}
	var n1, n2, oldest int
	d.QueryRow(`SELECT COUNT(*), MIN(published_at) FROM stock_news WHERE stock_id = 1`).Scan(&n1, &oldest)
	d.QueryRow(`SELECT COUNT(*) FROM stock_news WHERE stock_id = 2`).Scan(&n2)
	if n1 != tickertickKeepRows || oldest != 10 || n2 != tickertickKeepRows+10 {
		t.Errorf("stock 1 has %d rows from %d, stock 2 has %d; want %d from 10, and %d untouched",
			n1, oldest, n2, tickertickKeepRows, tickertickKeepRows+10)
	}
}
