package notify_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/notify"
)

// 10 am on Thu 29 Oct 2026 in the game time zone.
var (
	loc = time.FixedZone("AEDT", 11*60*60)
	now = time.Date(2026, 10, 29, 10, 0, 0, 0, loc)
)

// setup seeds Alice (1) and Bob (2). Email stays in the outbox.
func setup(t *testing.T) (*sql.DB, *db.Queries, *mail.Mailer) {
	t.Helper()
	d, err := handler.OpenDB(filepath.Join(t.TempDir(), "test.db"), game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	exec(t, d, `INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x.com', 'Alice', 't1'), (2, 'b@x.com', 'Bob', 't2')`)
	q := db.New(d)
	m, err := mail.New(q, mail.Log{}, mail.Config{BaseURL: "https://game.example"})
	if err != nil {
		t.Fatal(err)
	}
	return d, q, m
}

func exec(t *testing.T, d *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("%v\n%s", err, s)
		}
	}
}

type queued struct{ to, template, subject, text string }

func outbox(t *testing.T, d *sql.DB) []queued {
	t.Helper()
	rows, err := d.Query("SELECT to_email, template, subject, text_body FROM email_outbox ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []queued
	for rows.Next() {
		var e queued
		rows.Scan(&e.to, &e.template, &e.subject, &e.text)
		out = append(out, e)
	}
	return out
}

// seedGame inserts a game Alice leads (+10%) and Bob trails (-5%).
func seedGame(t *testing.T, d *sql.DB, status, start, end string) {
	t.Helper()
	exec(t, d,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date) VALUES (1, 1, 'Spring Cup', '`+status+`', 'USD', '`+start+`', '`+end+`')`,
		`INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (1, 1, 0, 110000000), (1, 2, 0, 95000000)`,
	)
}

func TestNotificationsAreBatchedByCategory(t *testing.T) {
	d, q, m := setup(t)
	seedGame(t, d, "active", "2026-10-01", "2026-12-31")
	exec(t, d, `INSERT INTO notifications (user_id, game_id, type, title, message) VALUES
		(1, 1, 'order_filled', 'Limit Order Filled', 'buy 10 shares of AAPL filled at $200.00'),
		(1, 1, 'order_filled', 'Queued Order Filled', 'sell 5 shares of MSFT filled at the open at $400.00'),
		(1, 1, 'achievement', 'Achievement Unlocked!', 'First Trade'),
		(2, 1, 'dividend', 'Dividend Received', 'AAPL paid $0.25 per share')`)

	before := now.Add(-5 * time.Hour) // 5 am: no game emails, but notifications still go
	notify.Run(context.Background(), q, m, before, loc)
	notify.Run(context.Background(), q, m, before, loc)

	got := outbox(t, d)
	if len(got) != 2 {
		t.Fatalf("queued %d emails, want 2 (one per user): %+v", len(got), got)
	}
	alice := got[0]
	if alice.to != "a@x.com" || alice.subject != "2 updates from StockGame" ||
		!strings.Contains(alice.text, "AAPL") || !strings.Contains(alice.text, "MSFT") ||
		!strings.Contains(alice.text, "https://game.example/games/1") {
		t.Errorf("Alice's email = %+v", alice)
	}
	if strings.Contains(alice.text, "First Trade") {
		t.Error("an achievement was emailed")
	}
	if got[1].subject != "Dividend Received: Spring Cup" {
		t.Errorf("Bob's subject = %q", got[1].subject)
	}
	var left int
	d.QueryRow("SELECT COUNT(*) FROM notifications WHERE emailed = 0").Scan(&left)
	if left != 0 {
		t.Errorf("%d notifications not marked emailed", left)
	}
}

func TestGameStartedOnceAfter8am(t *testing.T) {
	d, q, m := setup(t)
	seedGame(t, d, "active", "2026-10-29", "2026-11-30")

	notify.Run(context.Background(), q, m, now.Add(-3*time.Hour), loc)
	if got := outbox(t, d); len(got) != 0 {
		t.Fatalf("sent %d emails at 7 am", len(got))
	}
	notify.Run(context.Background(), q, m, now, loc)
	notify.Run(context.Background(), q, m, now.Add(time.Minute), loc)
	got := outbox(t, d)
	if len(got) != 2 {
		t.Fatalf("queued %d emails, want one each for Alice and Bob", len(got))
	}
	if got[0].subject != "Spring Cup has started" || !strings.Contains(got[0].text, "Mon 30 Nov") {
		t.Errorf("started email = %+v", got[0])
	}
}

func TestGameEndingTomorrow(t *testing.T) {
	d, q, m := setup(t)
	seedGame(t, d, "active", "2026-10-01", "2026-10-30")
	notify.Run(context.Background(), q, m, now, loc)
	got := outbox(t, d)
	if len(got) != 2 {
		t.Fatalf("queued %d emails, want 2", len(got))
	}
	if got[1].subject != "Spring Cup ends tomorrow" || !strings.Contains(got[1].text, "You're 2nd of 2") {
		t.Errorf("Bob's ending email = %+v", got[1])
	}
}

func TestResultsSkipOptedOutAndOldGames(t *testing.T) {
	d, q, m := setup(t)
	seedGame(t, d, "finished", "2026-10-01", "2026-10-28")
	exec(t, d,
		`INSERT INTO games (id, created_by, name, status, base_currency, start_date, end_date) VALUES (2, 1, 'Old Cup', 'finished', 'USD', '2026-01-01', '2026-02-01')`,
		`INSERT INTO participants (game_id, user_id, cash_balance, portfolio_value) VALUES (2, 1, 0, 1)`,
		`INSERT INTO email_prefs (user_id, category, enabled) VALUES (2, 'games', 0)`,
	)
	notify.Run(context.Background(), q, m, now, loc)
	got := outbox(t, d)
	if len(got) != 1 {
		t.Fatalf("queued %d emails, want only Alice's Spring Cup results: %+v", len(got), got)
	}
	if got[0].subject != "You won Spring Cup!" || !strings.Contains(got[0].text, "1. Alice: $1,100,000 (+10.0%)") ||
		!strings.Contains(got[0].text, "2. Bob: $950,000 (-5.0%)") {
		t.Errorf("results email = %+v", got[0])
	}
}
