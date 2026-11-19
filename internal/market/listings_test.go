package market

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// listingsStub answers /stocks with each exchange's list and /statistics
// with the market cap set for the symbol (404 for a symbol with none).
type listingsStub struct {
	lists map[string][]tdListing // by exchange
	caps  map[string]float64     // market cap by symbol
	asked map[string]int         // /statistics calls by symbol
}

func (s *listingsStub) RoundTrip(r *http.Request) (*http.Response, error) {
	var body any
	switch r.URL.Path {
	case "/stocks":
		body = map[string]any{"data": s.lists[r.URL.Query().Get("exchange")]}
	case "/statistics":
		sym := r.URL.Query().Get("symbol")
		s.asked[sym]++
		if c, ok := s.caps[sym]; ok {
			body = map[string]any{"statistics": map[string]any{"valuations_metrics": map[string]any{"market_capitalization": c}}}
		} else {
			body = map[string]any{"code": 404, "message": "Data not found", "status": "error"}
		}
	default:
		return nil, fmt.Errorf("unexpected request %s", r.URL)
	}
	b, _ := json.Marshal(body)
	return &http.Response{
		StatusCode: 200,
		Header:     http.Header{"Content-Type": {"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(b))),
		Request:    r,
	}, nil
}

// fillerList is n made-up common stocks on an exchange, so a list passes
// the length check.
func fillerList(exchange string, n int) []tdListing {
	out := make([]tdListing, n)
	for i := range out {
		out[i] = tdListing{Symbol: fmt.Sprintf("%s%04d", exchange[:1], i), Name: "Filler", Exchange: exchange, Type: "Common Stock"}
	}
	return out
}

// resetCredits empties the credit window, so a test never waits for it.
func resetCredits() {
	credits.mu.Lock()
	credits.spent = nil
	credits.mu.Unlock()
}

// TestCheckListings: the first run records a baseline and finds nothing.
// A later run finds new symbols, looks up their market caps, and reports
// only the S&P-size one, once. A symbol that hasn't traded is looked up
// again the next day; warrants are ignored; a stock already in the game
// isn't reported.
func TestCheckListings(t *testing.T) {
	d, q := monthlyDB(t)
	stub := &listingsStub{
		lists: map[string][]tdListing{
			"NASDAQ": fillerList("NASDAQ", 1500),
			"NYSE":   fillerList("NYSE", 500),
		},
		caps:  map[string]float64{},
		asked: map[string]int{},
	}
	f := &Fetcher{q: q, rawDB: d, client: &http.Client{Transport: stub}}
	ctx := context.Background()

	resetCredits()
	found, err := f.CheckListings(ctx, q, "2026-11-19")
	if err != nil || len(found) != 0 {
		t.Fatalf("baseline run: found %v, err %v", found, err)
	}
	if len(stub.asked) != 0 {
		t.Errorf("baseline run looked up %v", stub.asked)
	}

	stub.lists["NASDAQ"] = append(stub.lists["NASDAQ"],
		tdListing{Symbol: "ANT", Name: "Anthropic, PBC", Exchange: "NASDAQ", Type: "Common Stock"},
		tdListing{Symbol: "SMOL", Name: "Small Co", Exchange: "NASDAQ", Type: "Common Stock"},
		tdListing{Symbol: "SOON", Name: "Not Yet Inc", Exchange: "NASDAQ", Type: "Common Stock"},
		tdListing{Symbol: "SMOLW", Name: "Small Co Warrant", Exchange: "NASDAQ", Type: "Warrant"},
		tdListing{Symbol: "HAVE", Name: "Already In", Exchange: "NASDAQ", Type: "Common Stock"},
	)
	stub.caps["ANT"] = 61.5e9
	stub.caps["SMOL"] = 1.2e9
	stub.caps["HAVE"] = 90e9
	if _, err := d.Exec(`INSERT INTO stocks (symbol, name, market, currency, listing) VALUES ('HAVE', 'Already In', 'sp500', 'USD', 'us')`); err != nil {
		t.Fatal(err)
	}

	resetCredits()
	found, err = f.CheckListings(ctx, q, "2026-11-20")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Symbol != "ANT" || found[0].MarketCap != 61_500_000_000 {
		t.Fatalf("found %+v, want just ANT at 61.5 billion", found)
	}
	if stub.asked["SMOLW"] != 0 || stub.asked["HAVE"] != 0 {
		t.Errorf("looked up a warrant or a stock already in the game: %v", stub.asked)
	}

	resetCredits()
	found, err = f.CheckListings(ctx, q, "2026-11-23")
	if err != nil || len(found) != 0 {
		t.Fatalf("third run: found %v, err %v (ANT must be reported once)", found, err)
	}
	if stub.asked["ANT"] != 1 || stub.asked["SMOL"] != 1 {
		t.Errorf("a listing with a market cap was looked up again: %v", stub.asked)
	}
	if stub.asked["SOON"] != 2 {
		t.Errorf("SOON looked up %d times, want 2 (once a day until it trades)", stub.asked["SOON"])
	}
}

// TestCheckListingsShortList: a list much shorter than usual is a failed
// request, and nothing is saved, so the missing stocks don't look new later.
func TestCheckListingsShortList(t *testing.T) {
	d, q := monthlyDB(t)
	stub := &listingsStub{
		lists: map[string][]tdListing{"NASDAQ": fillerList("NASDAQ", 1500), "NYSE": fillerList("NYSE", 12)},
		caps:  map[string]float64{}, asked: map[string]int{},
	}
	f := &Fetcher{q: q, rawDB: d, client: &http.Client{Transport: stub}}
	resetCredits()
	if _, err := f.CheckListings(context.Background(), q, "2026-11-19"); err == nil {
		t.Fatal("a 12-stock NYSE list was accepted")
	}
	if n, _ := q.CountUSListings(context.Background()); n != 0 {
		t.Errorf("%d listings saved from a failed check", n)
	}
}

func TestSectorFor(t *testing.T) {
	for td, want := range map[string]string{
		"Technology": "Information Technology", "Financial Services": "Financials",
		"Consumer Cyclical": "Consumer Discretionary", "Basic Materials": "Materials",
		" healthcare ": "Healthcare", "Industrials": "Industrials", "Shell Companies": "",
	} {
		if got := SectorFor(td); got != want {
			t.Errorf("SectorFor(%q) = %q, want %q", td, got, want)
		}
	}
	for _, s := range tdSectors {
		found := false
		for _, x := range Sectors {
			found = found || x == s
		}
		if !found {
			t.Errorf("tdSectors maps to %q, which isn't in Sectors", s)
		}
	}
}

func TestBillions(t *testing.T) {
	for in, want := range map[int64]string{
		61_500_000_000:    "US$61.5 billion",
		20_000_000_000:    "US$20.0 billion",
		1_948_882_642_241: "US$1949 billion",
	} {
		if got := Billions(in); got != want {
			t.Errorf("Billions(%d) = %q, want %q", in, got, want)
		}
	}
}

// TestLoadListings: labels follow stocks.listing once loaded, so an admin
// moving a stock changes what players see.
func TestLoadListings(t *testing.T) {
	d, q := monthlyDB(t)
	t.Cleanup(func() { listings.Store(seedListingMap()) })
	if _, err := d.Exec(`INSERT INTO stocks (symbol, name, market, currency, listing) VALUES
		('ANT', 'Anthropic', 'sp500', 'USD', 'us'), ('BHP', 'BHP', 'sp500', 'USD', 'sp500')`); err != nil {
		t.Fatal(err)
	}
	if err := LoadListings(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if got := MarketLabel("ANT", "sp500"); got != "US" {
		t.Errorf("ANT labelled %q, want US", got)
	}
	if got := MarketLabel("BHP", "sp500"); got != "SP500" || IsAusUS("BHP", "sp500") {
		t.Errorf("BHP moved to the S&P 500 is labelled %q", got)
	}
}
