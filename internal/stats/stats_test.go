package stats

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/exploded/game"
	_ "modernc.org/sqlite"
)

func statsDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(game.Schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return d
}

// melbourne counts days in Melbourne time for one test, and starts with
// nothing held in memory.
func melbourne(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		t.Fatal(err)
	}
	SetLocation(loc)
	mu.Lock()
	pending = map[key]int64{}
	mu.Unlock()
	t.Cleanup(func() { SetLocation(time.UTC); now = time.Now })
	return loc
}

func count(t *testing.T, d *sql.DB, period, metric, item string) int64 {
	t.Helper()
	var n int64
	err := d.QueryRow("SELECT n FROM stat_counts WHERE period = ? AND metric = ? AND item = ?", period, metric, item).Scan(&n)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	return n
}

// Counts are held in memory and added to what's stored, never replacing it.
func TestFlushAdds(t *testing.T) {
	loc := melbourne(t)
	d := statsDB(t)
	ctx := context.Background()
	// 23:30 UTC on the 19th is 10:30 on the 20th in Melbourne.
	now = func() time.Time { return time.Date(2026, 11, 19, 23, 30, 0, 0, time.UTC) }

	Add(VideoPlay, "7")
	Add(VideoPlay, "7")
	Add(VideoPlay, "8")
	AddMonth(ActiveMonth)
	if err := Flush(ctx, d); err != nil {
		t.Fatal(err)
	}
	Add(VideoPlay, "7")
	if err := Flush(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := Flush(ctx, d); err != nil { // nothing held: writes nothing
		t.Fatal(err)
	}
	if n := count(t, d, "2026-11-20", VideoPlay, "7"); n != 3 {
		t.Errorf("video 7 on the 20th: %d, want 3", n)
	}
	if n := count(t, d, "2026-11-20", VideoPlay, "8"); n != 1 {
		t.Errorf("video 8: %d, want 1", n)
	}
	if n := count(t, d, "2026-11", ActiveMonth, ""); n != 1 {
		t.Errorf("active this month: %d, want 1", n)
	}
	if got := DayOf(now()); got != "2026-11-20" {
		t.Errorf("DayOf = %s in %s", got, loc)
	}
}

// A flush that fails keeps its counts for the next one.
func TestFlushKeepsCountsOnError(t *testing.T) {
	melbourne(t)
	ctx := context.Background()
	now = func() time.Time { return time.Date(2026, 11, 20, 1, 0, 0, 0, time.UTC) }
	broken := statsDB(t)
	broken.Close()
	Add(CalculatorView, "fhss")
	if err := Flush(ctx, broken); err == nil {
		t.Fatal("flush to a closed database didn't fail")
	}
	Add(CalculatorView, "fhss")
	d := statsDB(t)
	if err := Flush(ctx, d); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, "2026-11-20", CalculatorView, "fhss"); n != 2 {
		t.Errorf("fhss opens: %d, want 2 (the failed flush's count was lost)", n)
	}
}

// Rollup counts each finished day in Melbourne time, from the first sign-up,
// across a daylight saving change, and stores each day once.
func TestRollup(t *testing.T) {
	melbourne(t)
	d := statsDB(t)
	ctx := context.Background()
	// Daylight saving started at 2 am on Sunday 4 October 2026 (UTC+10 to
	// UTC+11), so that day ran from 14:00 UTC on the 3rd to 13:00 UTC on
	// the 4th.
	for _, stmt := range []string{
		`INSERT INTO users (id, email, name, email_token, created_at) VALUES
			(1, 'a@x', 'A', 't1', '2026-10-02 13:59:59'),  -- 23:59:59 on the 2nd (UTC+10)
			(2, 'b@x', 'B', 't2', '2026-10-03 14:00:00'),  -- midnight starting the 4th
			(3, 'c@x', 'C', 't3', '2026-10-04 12:30:00'),  -- 23:30 on the 4th (UTC+11)
			(4, 'd@x', 'D', 't4', '2026-10-04 13:30:00')`, // 00:30 on the 5th
		`INSERT INTO games (id, created_by, name, status, start_date, end_date) VALUES (1, 1, 'Oct', 'active', '2026-10-01', '2026-10-31')`,
		`INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 1, 0, 0), (2, 1, 2, 0, 0)`,
		`INSERT INTO stocks (id, symbol, name, market) VALUES (1, 'AAPL', 'Apple', 'sp500')`,
		`INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total, created_at) VALUES
			(1, 1, 'buy', 1, 1, 1, 1, '2026-10-04 01:00:00'),
			(1, 1, 'buy', 1, 1, 1, 1, '2026-10-04 02:00:00'),
			(2, 1, 'buy', 1, 1, 1, 1, '2026-10-04 03:00:00'),
			(1, 1, 'sell', 1, 1, 1, 1, '2026-10-05 01:00:00')`,
		`INSERT INTO lobby_messages (user_id, message, created_at, blocked_reason) VALUES
			(1, 'hi', '2026-10-04 01:00:00', ''), (1, 'rude', '2026-10-04 01:00:00', 'rude')`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatalf("seed: %v\n%s", err, stmt)
		}
	}

	// It's 09:00 on 6 October in Melbourne: the 2nd to the 5th are done.
	at := time.Date(2026, 10, 5, 22, 0, 0, 0, time.UTC)
	if err := Rollup(ctx, d, at); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		day, metric string
		n           int64
	}{
		{"2026-10-02", Signups, 1},
		{"2026-10-03", Signups, 0},
		{"2026-10-04", Signups, 2},
		{"2026-10-05", Signups, 1},
		{"2026-10-04", Trades, 3},
		{"2026-10-04", Traders, 2},
		{"2026-10-04", FirstTrades, 2},
		{"2026-10-05", Trades, 1},
		{"2026-10-05", FirstTrades, 0},
		{"2026-10-04", ChatPosted, 1},
		{"2026-10-04", ChatBlocked, 1},
	}
	for _, w := range want {
		if n := count(t, d, w.day, w.metric, ""); n != w.n {
			t.Errorf("%s %s: %d, want %d", w.day, w.metric, n, w.n)
		}
	}
	// A sign-up added to a stored day afterwards (or a row deleted) doesn't
	// change it, and running again stores nothing twice.
	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token, created_at) VALUES (5, 'e@x', 'E', 't5', '2026-10-04 01:00:00')`); err != nil {
		t.Fatal(err)
	}
	if err := Rollup(ctx, d, at); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d, "2026-10-04", Signups, ""); n != 2 {
		t.Errorf("4 October after a rerun: %d sign-ups, want 2", n)
	}
	// The next day it stores the 6th only.
	if err := Rollup(ctx, d, at.AddDate(0, 0, 1)); err != nil {
		t.Fatal(err)
	}
	var last string
	d.QueryRow("SELECT value FROM media_state WHERE key = ?", rollupDayKey).Scan(&last)
	if last != "2026-10-06" {
		t.Errorf("last day stored: %q, want 2026-10-06", last)
	}
}

// With no users there's nothing to count.
func TestRollupEmpty(t *testing.T) {
	melbourne(t)
	d := statsDB(t)
	if err := Rollup(context.Background(), d, time.Now()); err != nil {
		t.Fatal(err)
	}
	var n int
	d.QueryRow("SELECT COUNT(*) FROM stat_counts").Scan(&n)
	if n != 0 {
		t.Errorf("%d counts stored for an empty site", n)
	}
}
