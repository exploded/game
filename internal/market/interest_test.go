package market

import (
	"context"
	"testing"
	"time"
)

func TestDailyInterest(t *testing.T) {
	for _, c := range []struct{ balance, bps, want int64 }{
		{100000000, 100, 2740}, // $1M at 1%: $27.397 a day
		{100000000, 500, 13699},
		{50000, 100, 1}, // $500: 1.37c
		{1000, 100, 0},
		{100000000, 0, 0},
	} {
		if got := DailyInterest(c.balance, c.bps); got != c.want {
			t.Errorf("DailyInterest(%d, %d) = %d, want %d", c.balance, c.bps, got, c.want)
		}
	}
}

// TestCreditInterest: the scheduler runs every minute, so each player is
// paid once for the day that ended, and only if they were in the game that
// day with cash to earn on.
func TestCreditInterest(t *testing.T) {
	d, q := monthlyDB(t)
	ctx := context.Background()
	if _, err := d.Exec(`
		INSERT INTO users (id, email, name, email_token) VALUES
			(1, 'a@x', 'Alice', 't1'), (2, 'b@x', 'Bob', 't2'), (3, 'c@x', 'Cat', 't3');
		INSERT INTO games (id, created_by, name, status, start_date, end_date, credit_interest_rate) VALUES
			(1, 1, 'Oct–Dec 2026', 'active', '2026-10-01', '2026-12-31', 100);
		-- Midnight on 31 October in Melbourne is 13:00 UTC on the 30th.
		INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value, joined_at) VALUES
			(1, 1, 1, 100000000, 120000000, '2026-10-30 12:59:00'),
			(2, 1, 2, 100000000, 100000000, '2026-10-30 13:30:00'),
			(3, 1, 3, 0, 100000000, '2026-10-01 00:00:00');`); err != nil {
		t.Fatal(err)
	}

	aedt := time.FixedZone("AEDT", 11*60*60)
	for _, m := range []int{1, 2, 3} {
		if err := CreditInterest(ctx, q, d, time.Date(2026, 10, 31, 0, m, 0, 0, aedt)); err != nil {
			t.Fatal(err)
		}
	}

	want := map[int64][2]int64{
		1: {100002740, 120002740}, // joined before midnight: paid once
		2: {100000000, 100000000}, // joined after midnight
		3: {0, 100000000},         // no cash
	}
	for id, w := range want {
		var cash, value int64
		d.QueryRow("SELECT cash_balance, portfolio_value FROM participants WHERE id = ?", id).Scan(&cash, &value)
		if cash != w[0] || value != w[1] {
			t.Errorf("player %d: cash %d, value %d; want %d, %d", id, cash, value, w[0], w[1])
		}
	}
	var day string
	var amount int64
	if err := d.QueryRow("SELECT date, amount FROM interest_payments WHERE participant_id = 1").Scan(&day, &amount); err != nil {
		t.Fatal(err)
	}
	if day != "2026-10-30" || amount != 2740 {
		t.Errorf("payment for %s of %d, want 2026-10-30 of 2740", day, amount)
	}
	if total, _ := q.TotalInterest(ctx, 1); total != 2740 {
		t.Errorf("TotalInterest = %d, want 2740", total)
	}

	// The day after the game's last day is paid for that last day; the day
	// after that, nothing.
	for _, now := range []time.Time{
		time.Date(2027, 1, 1, 0, 1, 0, 0, aedt),
		time.Date(2027, 1, 2, 0, 1, 0, 0, aedt),
	} {
		if err := CreditInterest(ctx, q, d, now); err != nil {
			t.Fatal(err)
		}
	}
	var days []string
	rows, _ := d.Query("SELECT date FROM interest_payments WHERE participant_id = 1 ORDER BY date")
	for rows.Next() {
		var s string
		rows.Scan(&s)
		days = append(days, s)
	}
	rows.Close()
	if len(days) != 2 || days[1] != "2026-12-31" {
		t.Errorf("paid for %q, want 2026-10-30 and 2026-12-31", days)
	}
}
