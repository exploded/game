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

	oldThumb := fetchNewsThumb
	var thumbAsked []string
	fetchNewsThumb = func(_ context.Context, u string) (string, []byte, error) {
		thumbAsked = append(thumbAsked, u)
		return "image/jpeg", []byte("jpeg"), nil
	}
	defer func() { fetchNewsThumb = oldThumb }()

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

	// Each shown story got its picture once, while it was one of the newest
	// two, and alexa's went when results pushed it out.
	if got := fmt.Sprint(thumbAsked); got != "[https://pymnts.com/alexa https://qz.com/loan https://cnbc.com/results]" {
		t.Errorf("thumbnails asked for %s", got)
	}
	var thumbs int
	d.QueryRow(`SELECT COUNT(*) FROM news_thumbs`).Scan(&thumbs)
	if thumbs != NewsShown {
		t.Errorf("%d thumbnails kept, want %d", thumbs, NewsShown)
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

func TestCanonicalNewsURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.fool.com/earnings/call-transcripts/2026/11/05/cardinal-health-cah-q1-2027-earnings-call-transcript/?source=iedfolrf0000001": "https://www.fool.com/earnings/call-transcripts/2026/11/05/cardinal-health-cah-q1-2027-earnings-call-transcript/",
		"https://example.com/a?utm_source=x&utm_medium=y&id=7#top":                                                                               "https://example.com/a?id=7",
		"https://example.com/a?id=7":                               "https://example.com/a?id=7",
		"https://www.barchart.com/story/news/8623450/x?.tsrc=rss":  "https://www.barchart.com/story/news/8623450/x",
		"http://www.businesswire.com/news/home/1/en/X/?feedref=Jj": "http://www.businesswire.com/news/home/1/en/X/",
		"https://example.com/a":                                    "https://example.com/a",
		"not a url":                                                "not a url",
	} {
		if got := canonicalNewsURL(in); got != want {
			t.Errorf("canonicalNewsURL(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNewStoriesRepeats: a story is a repeat when its URL matches without
// tracking tags, or its site already sent the headline, in this batch or
// stored.
func TestNewStoriesRepeats(t *testing.T) {
	const transcript = "https://www.fool.com/earnings/call-transcripts/2026/11/05/cardinal-health-cah-q1-2027-earnings-call-transcript/"
	stories := []tickertickStory{
		{Title: "Cardinal Health (CAH) Q1 2027 Earnings Call Transcript", URL: transcript, Site: "fool.com"},
		{Title: "Cardinal Health (CAH) Q1 2027 Earnings Call Transcript", URL: transcript + "?source=iedfolrf0000001", Site: "fool.com"},
		{Title: "Cardinal  Health (CAH) q1 2027 earnings call transcript", URL: "https://www.fool.com/other/", Site: "fool.com"},
		{Title: "Cardinal Health (CAH) Q1 2027 Earnings Call Transcript", URL: "https://www.cnbc.com/cah", Site: "cnbc.com"},
		{Title: "Old story", URL: "https://www.cnbc.com/old?utm_source=feed", Site: "cnbc.com"},
		{Title: "Old headline, new link", URL: "https://www.cnbc.com/new", Site: "cnbc.com"},
	}
	known := []db.ListStockNewsKeysRow{
		{Url: "https://www.cnbc.com/old", Source: "cnbc.com", Headline: "Old story, first headline"},
		{Url: "https://www.cnbc.com/elsewhere", Source: "cnbc.com", Headline: "Old headline, new link"},
	}
	var got []string
	for _, st := range newStories(stories, known) {
		got = append(got, st.Site+" "+st.URL)
	}
	want := "[fool.com " + transcript + " cnbc.com https://www.cnbc.com/cah]"
	if fmt.Sprint(got) != want {
		t.Errorf("newStories = %s, want %s", got, want)
	}
}
