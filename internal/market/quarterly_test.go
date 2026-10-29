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

func TestQuarterBoundaries(t *testing.T) {
	for date, want := range map[string]struct{ key, name, start, end string }{
		"2027-01-01": {"2027-Q1", "Jan–Mar 2027", "2027-01-01", "2027-03-31"},
		"2027-03-31": {"2027-Q1", "Jan–Mar 2027", "2027-01-01", "2027-03-31"},
		"2027-04-01": {"2027-Q2", "Apr–Jun 2027", "2027-04-01", "2027-06-30"},
		"2027-09-30": {"2027-Q3", "Jul–Sep 2027", "2027-07-01", "2027-09-30"},
		"2026-12-31": {"2026-Q4", "Oct–Dec 2026", "2026-10-01", "2026-12-31"},
	} {
		d, _ := time.Parse(time.DateOnly, date)
		q := QuarterOf(d)
		if q.Key() != want.key || q.Name() != want.name || q.StartDate() != want.start || q.EndDate() != want.end {
			t.Errorf("%s: got %s %q %s to %s, want %s %q %s to %s", date,
				q.Key(), q.Name(), q.StartDate(), q.EndDate(), want.key, want.name, want.start, want.end)
		}
	}
	if next := (Quarter{2026, 4}).Next(); next != (Quarter{2027, 1}) {
		t.Errorf("after 2026-Q4 got %s, want 2027-Q1", next.Key())
	}
}

func quarterlyDB(t *testing.T) (*sql.DB, *db.Queries) {
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

// TestQuarterlyGamesCreatedOnce: the scheduler runs every minute, so each
// quarter gets exactly one game, and the next quarter's copies the settings
// an admin gave the one before.
func TestQuarterlyGamesCreatedOnce(t *testing.T) {
	d, q := quarterlyDB(t)
	ctx := context.Background()

	// No users yet: nothing to create the game as.
	EnsureQuarterlyGames(ctx, q, time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	var n int
	d.QueryRow("SELECT COUNT(*) FROM games").Scan(&n)
	if n != 0 {
		t.Fatalf("created %d games with no users, want 0", n)
	}

	if _, err := d.Exec(`INSERT INTO users (id, email, name, email_token, is_admin) VALUES (1, 'a@x', 'Alice', 't1', 0), (2, 'b@x', 'Bob', 't2', 1)`); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		EnsureQuarterlyGames(ctx, q, time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC))
	}
	rows, err := d.Query("SELECT quarter, name, start_date, end_date, created_by, limit_orders FROM games ORDER BY start_date")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for rows.Next() {
		var key, name, start, end string
		var by, limits int
		rows.Scan(&key, &name, &start, &end, &by, &limits)
		if by != 2 || limits != 1 {
			t.Errorf("%s: created by %d with limit orders %d, want the admin (2) and 1", key, by, limits)
		}
		got = append(got, key+" "+name+" "+start+" "+end)
	}
	rows.Close()
	want := []string{"2026-Q4 Oct–Dec 2026 2026-10-01 2026-12-31", "2027-Q1 Jan–Mar 2027 2027-01-01 2027-03-31"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("games = %q, want %q", got, want)
	}

	// An admin changes the Jan–Mar game; Apr–Jun copies it on 1 January.
	if _, err := d.Exec(`UPDATE games SET trade_fee = 995, starting_balance = 50000000 WHERE quarter = '2027-Q1'`); err != nil {
		t.Fatal(err)
	}
	EnsureQuarterlyGames(ctx, q, time.Date(2027, 1, 1, 0, 1, 0, 0, time.UTC))
	var fee, balance int
	var start string
	if err := d.QueryRow("SELECT trade_fee, starting_balance, start_date FROM games WHERE quarter = '2027-Q2'").Scan(&fee, &balance, &start); err != nil {
		t.Fatalf("no Apr–Jun game: %v", err)
	}
	if fee != 995 || balance != 50000000 || start != "2027-04-01" {
		t.Errorf("Apr–Jun game has fee %d, balance %d, start %s; want 995, 50000000, 2027-04-01", fee, balance, start)
	}
}
