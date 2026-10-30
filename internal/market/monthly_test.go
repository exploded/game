package market

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
)

func TestMonthBoundaries(t *testing.T) {
	for date, want := range map[string]struct{ key, name, start, end string }{
		"2026-11-01": {"2026-11", "November 2026", "2026-11-01", "2026-11-30"},
		"2026-12-31": {"2026-12", "December 2026", "2026-12-01", "2026-12-31"},
		"2027-02-14": {"2027-02", "February 2027", "2027-02-01", "2027-02-28"},
		"2028-02-29": {"2028-02", "February 2028", "2028-02-01", "2028-02-29"},
	} {
		d, _ := time.Parse(time.DateOnly, date)
		m := MonthOf(d)
		if m.Key() != want.key || m.Name() != want.name || m.StartDate() != want.start || m.EndDate() != want.end {
			t.Errorf("%s: got %s %q %s to %s, want %s %q %s to %s", date,
				m.Key(), m.Name(), m.StartDate(), m.EndDate(), want.key, want.name, want.start, want.end)
		}
	}
	if next := (Month{2026, time.December}).Next(); next != (Month{2027, time.January}) {
		t.Errorf("after 2026-12 got %s, want 2027-01", next.Key())
	}
}

func monthlyDB(t *testing.T) (*sql.DB, *db.Queries) {
	t.Helper()
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(game.Schema); err != nil {
		t.Fatal(err)
	}
	return d, db.New(d)
}

// TestMonthlyGamesCreatedOnce: the scheduler runs every minute, so each
// month gets exactly one game, and the next month's copies the settings an
// admin gave the one before.
func TestMonthlyGamesCreatedOnce(t *testing.T) {
	d, q := monthlyDB(t)
	ctx := context.Background()

	// No users yet: nothing to create the game as.
	EnsureMonthlyGames(ctx, q, time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	var n int
	d.QueryRow("SELECT COUNT(*) FROM games").Scan(&n)
	if n != 0 {
		t.Fatalf("created %d games with no users, want 0", n)
	}

	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token, is_admin) VALUES (1, 'a@x', 'Alice', 't1', 0), (2, 'b@x', 'Bob', 't2', 1)`); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		EnsureMonthlyGames(ctx, q, time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	}
	rows, err := d.Query("SELECT quarter, name, start_date, end_date, created_by, limit_orders, after_hours_trading FROM games ORDER BY start_date")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var key, name, start, end string
		var by, limits, afterHours int
		rows.Scan(&key, &name, &start, &end, &by, &limits, &afterHours)
		if by != 2 || limits != 1 || afterHours != 1 {
			t.Errorf("%s: created by %d with limit orders %d, after hours %d; want the admin (2), 1 and 1", key, by, limits, afterHours)
		}
		got = append(got, key+" "+name+" "+start+" "+end)
	}
	rows.Close()
	want := []string{"2026-10 October 2026 2026-10-01 2026-10-31", "2026-11 November 2026 2026-11-01 2026-11-30"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("games = %q, want %q", got, want)
	}

	// An admin changes November's game; December's copies it on 1 November.
	if _, err := d.Exec(`UPDATE games SET trade_fee = 995, starting_balance = 50000000, after_hours_trading = 0 WHERE quarter = '2026-11'`); err != nil {
		t.Fatal(err)
	}
	EnsureMonthlyGames(ctx, q, time.Date(2026, 11, 1, 0, 1, 0, 0, time.UTC))
	var fee, balance, afterHours int
	var start string
	if err := d.QueryRow("SELECT trade_fee, starting_balance, after_hours_trading, start_date FROM games WHERE quarter = '2026-12'").Scan(&fee, &balance, &afterHours, &start); err != nil {
		t.Fatalf("no December game: %v", err)
	}
	if fee != 995 || balance != 50000000 || afterHours != 0 || start != "2026-12-01" {
		t.Errorf("December game has fee %d, balance %d, after hours %d, start %s; want 995, 50000000, 0, 2026-12-01", fee, balance, afterHours, start)
	}
}
