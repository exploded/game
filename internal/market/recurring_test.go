package market

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
)

// TestRecurringGameMakesOneNextRound: the scheduler runs every minute, so a
// finished recurring game must produce its next round once, with the same
// settings.
func TestRecurringGameMakesOneNextRound(t *testing.T) {
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(game.Schema); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	for _, s := range []string{
		`INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x', 'Alice', 't1')`,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date, recurring_interval, is_public, limit_orders)
		 VALUES (1, 1, 'Weekly Cup', 'finished', 'USD', '2026-10-19', '2026-10-25', 'weekly', 1, 1)`,
	} {
		if _, err := d.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		ProcessRecurringGames(context.Background(), q)
	}
	var n int
	var start, parent string
	var public, limits int
	d.QueryRow("SELECT COUNT(*) FROM games WHERE id != 1").Scan(&n)
	d.QueryRow("SELECT start_date, parent_game_id, is_public, limit_orders FROM games WHERE id != 1").Scan(&start, &parent, &public, &limits)
	if n != 1 || start != "2026-10-26" || parent != "1" || public != 1 || limits != 1 {
		t.Errorf("got %d new games (start %s, parent %s, public %d, limit orders %d), want one starting 2026-10-26 with game 1's settings", n, start, parent, public, limits)
	}
}
