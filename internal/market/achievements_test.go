package market

import (
	"context"
	"testing"
)

// TestAwardPlaces: Ann wins, Ben and Cat tie for 2nd, Dan is 4th. Eve would
// be 1st but never traded, and Fay is blocked, so neither is placed.
func TestAwardPlaces(t *testing.T) {
	d, q := monthlyDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`
		INSERT INTO users (id, email, name, email_token, blocked_at) VALUES
			(1, 'a@x', 'Ann', 't1', NULL), (2, 'b@x', 'Ben', 't2', NULL), (3, 'c@x', 'Cat', 't3', NULL),
			(4, 'd@x', 'Dan', 't4', NULL), (5, 'e@x', 'Eve', 't5', NULL), (6, 'f@x', 'Fay', 't6', '2026-10-01');
		INSERT INTO games (id, created_by, name, status, start_date, end_date) VALUES
			(1, 1, 'October 2026', 'active', '2026-10-01', '2026-10-31');
		INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES
			(1, 1, 1, 0, 130), (2, 1, 2, 0, 120), (3, 1, 3, 0, 120),
			(4, 1, 4, 0, 110), (5, 1, 5, 0, 200), (6, 1, 6, 0, 300);
		INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'ACME', 'Acme', 'sp500', 'USD');
		INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total) VALUES
			(1, 1, 'buy', 1, 1, 1, 1), (2, 1, 'buy', 1, 1, 1, 1), (3, 1, 'buy', 1, 1, 1, 1),
			(4, 1, 'buy', 1, 1, 1, 1), (6, 1, 'buy', 1, 1, 1, 1);
		INSERT INTO achievements (key, name, description) VALUES
			('first_place', 'Champion', ''), ('top_three', 'Podium Finish', '');`); err != nil {
		t.Fatal(err)
	}

	finished, err := q.FinishExpiredGames(ctx, "2026-11-01")
	if err != nil || len(finished) != 1 {
		t.Fatalf("finished %d games, err %v", len(finished), err)
	}
	AwardPlaces(ctx, q, finished[0])
	AwardPlaces(ctx, q, finished[0]) // awarded once only

	rows, err := d.Query(`SELECT ua.user_id, a.key FROM user_achievements ua
		JOIN achievements a ON a.id = ua.achievement_id ORDER BY ua.user_id, a.key`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var user int
		var key string
		rows.Scan(&user, &key)
		got = append(got, string(rune('0'+user))+" "+key)
	}
	want := []string{"1 first_place", "1 top_three", "2 top_three", "3 top_three"}
	if len(got) != len(want) {
		t.Fatalf("awarded %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("awarded %v, want %v", got, want)
		}
	}

	var notes int
	d.QueryRow("SELECT COUNT(*) FROM notifications WHERE type = 'achievement'").Scan(&notes)
	if notes != 4 {
		t.Errorf("%d notifications, want 4", notes)
	}
}
