package handler

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
)

func TestNavTab(t *testing.T) {
	for path, want := range map[string]string{
		"/games/7":              "home",
		"/games/7/history":      "home",
		"/games/7/rules":        "home",
		"/games/7/orders":       "home",
		"/games/7/stocks":       "stocks",
		"/games/7/stocks/12":    "stocks",
		"/games/7/leaderboard":  "ranks",
		"/games/7/activity":     "ranks",
		"/learn":                "learn",
		"/learn/questions":      "learn",
		"/courses/super/3":      "learn",
		"/learning":             "",
		"/lobby":                "chat",
		"/notifications":        "notifications",
		"/settings":             "",
		"/games":                "",
		"/games/7/export/trade": "",
	} {
		if got := navTab(path); got != want {
			t.Errorf("navTab(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestDaysLeft(t *testing.T) {
	g := db.Game{Status: "active", StartDate: "2026-11-01", EndDate: "2026-11-30"}
	at := func(day string) time.Time {
		d, _ := time.ParseInLocation(time.DateTime, day, time.UTC)
		return d
	}
	for _, c := range []struct {
		g    db.Game
		now  string
		want string
	}{
		{g, "2026-11-03 09:00:00", "28 days left"},
		{g, "2026-11-29 23:59:00", "2 days left"},
		{g, "2026-11-30 00:01:00", "Last day"},
		{db.Game{Status: "pending", StartDate: "2026-12-01", EndDate: "2026-12-31"}, "2026-11-03 09:00:00", "Starts 1 Dec"},
		{db.Game{Status: "finished", StartDate: "2026-10-01", EndDate: "2026-10-31"}, "2026-11-03 09:00:00", "Finished"},
	} {
		if got := daysLeft(c.g, at(c.now)); got != c.want {
			t.Errorf("daysLeft(%s, %s) = %q, want %q", c.g.Status, c.now, got, c.want)
		}
	}
}

// TestSectorShares: biggest first, a missing sector is Other, and shorts
// aren't counted.
func TestSectorShares(t *testing.T) {
	v := func(f float64) sql.NullFloat64 { return sql.NullFloat64{Float64: f, Valid: true} }
	got := sectorShares([]db.ListHoldingsBySectorRow{
		{Sector: "Energy", TotalValue: v(25000)},
		{Sector: "", TotalValue: v(25000)},
		{Sector: "Information Technology", TotalValue: v(50000)},
		{Sector: "Financials", TotalValue: v(-30000)},
	})
	if len(got) != 3 || got[0].Name != "Information Technology" || got[0].Pct != 50 || got[2].Name != "Other" || got[2].Pct != 25 {
		t.Errorf("sectorShares = %+v", got)
	}
}

// TestNavIsRenderedWithThePage: the bell's count comes with the page, not a
// request after it (which made the bar jump), and the tab bar shows on a
// game page.
func TestNavIsRenderedWithThePage(t *testing.T) {
	srv, q := gameServer(t)
	if err := q.CreateNotification(t.Context(), db.CreateNotificationParams{UserID: 1, Type: "trade", Title: "Filled", Message: "x"}); err != nil {
		t.Fatal(err)
	}
	body := getPage(t, srv, "/games/1/activity", "")
	for _, want := range []string{
		`<span class="bell-count">1</span>`,
		`<nav class="dock" aria-label="Main">`,
		`<a href="/games/1/activity" aria-current="page">Activity</a>`, // under Ranks
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
	if strings.Contains(body, "/notifications/count") {
		t.Error("the bell still fetches its count after the page loads")
	}
}

// TestWatchlistChip lists only the player's watched stocks, and says how to
// add one when there are none.
func TestWatchlistChip(t *testing.T) {
	srv, q := gameServer(t)
	body := getPage(t, srv, "/games/1/stocks?f=watch", "")
	if !strings.Contains(body, "Nothing on your watchlist yet.") || strings.Contains(body, "<strong>ACME</strong>") {
		t.Error("an empty watchlist doesn't say how to add to it")
	}
	if err := q.AddToWatchlist(t.Context(), db.AddToWatchlistParams{UserID: 1, StockID: 1}); err != nil {
		t.Fatal(err)
	}
	if body := getPage(t, srv, "/games/1/stocks?f=watch", ""); !strings.Contains(body, "<strong>ACME</strong>") {
		t.Error("the Watchlist chip doesn't list a watched stock")
	}
	if _, err := q.JoinGame(t.Context(), db.JoinGameParams{GameID: 1, UserID: 2, CashBalance: 100000000, PortfolioValue: 100000000}); err != nil {
		t.Fatal(err)
	}
	if body := getPage(t, srv, "/games/1/stocks?f=watch", "2"); strings.Contains(body, "<strong>ACME</strong>") {
		t.Error("another player's watchlist shows")
	}
}

// TestHomeWaitingOrders: open orders show on the home page, and only then.
func TestHomeWaitingOrders(t *testing.T) {
	srv, q := gameServer(t)
	if body := getPage(t, srv, "/games/1", ""); strings.Contains(body, "Waiting orders") {
		t.Error("Waiting orders shows with no open orders")
	}
	p, _ := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	if _, err := q.CreateLimitOrder(t.Context(), db.CreateLimitOrderParams{
		ParticipantID: p.ID, StockID: 1, Type: "buy", Quantity: 5, LimitPrice: 900,
	}); err != nil {
		t.Fatal(err)
	}
	body := getPage(t, srv, "/games/1", "")
	if !strings.Contains(body, "Waiting orders") || !strings.Contains(body, "Limit $9.00") {
		t.Error("an open limit order isn't on the home page")
	}
}

// TestPctChange: a change too small to show is 0.00, not -0.00.
func TestPctChange(t *testing.T) {
	for _, c := range []struct {
		v, total int64
		want     string
	}{
		{99996289, 100000000, "0.00"}, // -$37.11 on $1M
		{100003000, 100000000, "0.00"},
		{100125000, 100000000, "+0.13"},
		{98000000, 100000000, "-2.00"},
		{5, 0, "0.00"},
	} {
		if got := pctChange(c.v, c.total); got != c.want {
			t.Errorf("pctChange(%d, %d) = %q, want %q", c.v, c.total, got, c.want)
		}
	}
}

// TestHomeInGameCurrency: in an Australian dollar game, a US stock's value
// and gains are in Australian dollars beside the cash, its own value and
// prices marked US$, and the percentages are the share price's.
func TestHomeInGameCurrency(t *testing.T) {
	srv, q, d := gameServerDB(t)
	for _, stmt := range []string{
		`UPDATE games SET base_currency = 'AUD' WHERE id = 1`,
		`INSERT INTO exchange_rates (date, rate_aud_usd) VALUES ('2026-10-28', 650000)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	p, _ := q.GetParticipantByGameAndUser(t.Context(), db.GetParticipantByGameAndUserParams{GameID: 1, UserID: 1})
	// 10 ACME bought for US$90.00, now worth US$105.00.
	if err := q.UpsertHolding(t.Context(), db.UpsertHoldingParams{
		ParticipantID: p.ID, StockID: 1, Quantity: 10, AvgCost: 900, CostBasis: 9000, CurrentValue: 10500,
	}); err != nil {
		t.Fatal(err)
	}
	body := getPage(t, srv, "/games/1", "")
	for _, want := range []string{
		`$161.53 <span class="text-muted c-cur">US$105.00</span>`,                              // A$ = US$ / 0.65
		`<span class="pill pill-up">&#43;16.67%</span> <span class="c-amt">&#43;$23.07</span>`, // US$15.00 gained
		`US$10.50`, // the price
		`US$9.00`,  // the average cost
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}
