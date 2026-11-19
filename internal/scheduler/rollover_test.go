package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
	_ "time/tzdata" // Australia/Melbourne on any machine

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/handler"
	"github.com/exploded/game/internal/jobs"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
)

func schedulerDB(t *testing.T) (*sql.DB, *db.Queries) {
	t.Helper()
	d, err := handler.OpenDB(filepath.Join(t.TempDir(), "test.db"), game.Schema)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, db.New(d)
}

func exec(t *testing.T, d *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := d.Exec(s); err != nil {
			t.Fatalf("%v\n%s", err, s)
		}
	}
}

func count(t *testing.T, d *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := d.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%v\n%s", err, query)
	}
	return n
}

// TestMonthRollover runs the scheduler's minute across midnight at the end
// of November in Melbourne (11 hours ahead of UTC, so the UTC date is still
// the 30th): the last day's interest is paid once, the game's open orders
// are cancelled, it finishes and awards places, December's game is there,
// and the results and started emails go once each, after 8 am.
func TestMonthRollover(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		t.Fatal(err)
	}
	if market.NewYork == nil {
		t.Skip("no America/New_York time zone")
	}
	// Keep the Twelve Data jobs out of it: the listings check is off with
	// TWELVEDATA_SYMBOLS, and the splits check is marked done for the New
	// York day (30 November throughout).
	t.Setenv("TWELVEDATA_SYMBOLS", "TEST")
	splitsCheckedFor.Store("2026-11-30")
	t.Cleanup(func() { splitsCheckedFor.Store("") })

	d, q := schedulerDB(t)
	ctx := context.Background()
	handler.SeedAchievements(ctx, q)
	m, err := mail.New(q, mail.Log{}, mail.Config{BaseURL: "https://game.example"})
	if err != nil {
		t.Fatal(err)
	}
	exec(t, d, `INSERT INTO users (id, email, name, email_token, is_admin) VALUES
		(1, 'rob@x.com', 'Rob', 't1', 1), (2, 'alice@x.com', 'Alice', 't2', 0), (3, 'bob@x.com', 'Bob', 't3', 0)`)
	nov30 := func(hour, minute int) time.Time { return time.Date(2026, 11, 30, hour, minute, 0, 0, loc) }
	dec1 := func(hour, minute int) time.Time { return time.Date(2026, 12, 1, hour, minute, 0, 0, loc) }
	tick := func(now time.Time) {
		t.Helper()
		run(ctx, q, d, loc, m, nil, now)
		if !jobs.Wait(10 * time.Second) {
			t.Fatal("background jobs still running")
		}
	}

	// 11:58 pm on the 30th: November's game is made and started, and
	// December's is waiting.
	tick(nov30(23, 58))
	var nov, dec db.Game
	if nov, err = q.GetGameByMonth(ctx, sql.NullString{String: "2026-11", Valid: true}); err != nil {
		t.Fatal(err)
	}
	if nov.Status != "active" {
		t.Fatalf("November's game is %s, want active", nov.Status)
	}
	// Alice leads Bob; both have traded, so both are ranked. Bob has an
	// order waiting.
	exec(t, d,
		`INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'ACME', 'Acme', 'sp500', 'USD')`,
		`INSERT INTO participants (id, game_id, user_id, cash_balance, portfolio_value) VALUES
			(1, `+strconv.FormatInt(nov.ID, 10)+`, 2, 50000000, 110000000), (2, `+strconv.FormatInt(nov.ID, 10)+`, 3, 40000000, 95000000)`,
		`INSERT INTO transactions (participant_id, stock_id, type, quantity, price, total, converted_total) VALUES
			(1, 1, 'buy', 1, 1, 1, 1), (2, 1, 'buy', 1, 1, 1, 1)`,
		`INSERT INTO limit_orders (participant_id, stock_id, type, quantity, limit_price) VALUES (2, 1, 'buy', 5, 100)`,
	)

	tick(nov30(23, 59))
	if n := count(t, d, `SELECT COUNT(*) FROM interest_payments WHERE date = '2026-11-30'`); n != 0 {
		t.Fatalf("30 November's interest paid before it ended: %d", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM limit_orders WHERE status = 'open'`); n != 1 {
		t.Fatalf("%d open orders before the game ended, want 1", n)
	}

	// Just after midnight: the 30th's interest, then the end of the game.
	tick(dec1(0, 1))
	tick(dec1(0, 2))
	if n := count(t, d, `SELECT COUNT(*) FROM interest_payments WHERE date = '2026-11-30'`); n != 2 {
		t.Errorf("%d interest payments for 30 November, want one each for Alice and Bob", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM interest_payments WHERE amount <= 0`); n != 0 {
		t.Errorf("%d interest payments of nothing on $400,000+ cash", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM limit_orders WHERE status = 'cancelled'`); n != 1 {
		t.Errorf("%d orders cancelled when the game ended, want 1", n)
	}
	if nov, err = q.GetGame(ctx, nov.ID); err != nil || nov.Status != "finished" {
		t.Errorf("November's game: %s, %v; want finished", nov.Status, err)
	}
	if dec, err = q.GetGameByMonth(ctx, sql.NullString{String: "2026-12", Valid: true}); err != nil || dec.Status != "active" {
		t.Errorf("December's game: %+v, %v; want active", dec.Status, err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM games WHERE quarter = '2027-01'`); n != 1 {
		t.Errorf("%d games for January, want 1 (the next one to join)", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM user_achievements ua JOIN achievements a ON a.id = ua.achievement_id
		WHERE ua.user_id = 2 AND a.key = 'first_place'`); n != 1 {
		t.Errorf("Alice has %d first places, want 1", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM email_outbox WHERE template IN ('game_results', 'game_started')`); n != 0 {
		t.Errorf("%d game emails before 8 am", n)
	}

	// 8:16 am: results to November's players, and December's start to
	// everyone, once each however many minutes run.
	tick(dec1(8, 16))
	tick(dec1(8, 17))
	if n := count(t, d, `SELECT COUNT(*) FROM email_outbox WHERE template = 'game_results'`); n != 2 {
		t.Errorf("%d results emails, want one each for Alice and Bob", n)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM email_outbox WHERE template = 'game_started'`); n != 3 {
		t.Errorf("%d started emails, want one each for Rob, Alice and Bob", n)
	}
}

// fakeUploader stands in for the S3 backup.
type fakeUploader struct {
	mu    sync.Mutex
	fail  error
	calls []string
}

func (f *fakeUploader) Upload(_ context.Context, day string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, day)
	if f.fail != nil {
		return 0, f.fail
	}
	return 123, nil
}

func (f *fakeUploader) Bucket() string { return "test-bucket" }

func (f *fakeUploader) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// TestMaybeBackup: the backup runs once a day from 3 am, a failure is
// tried again an hour later, and only a good one records the day.
func TestMaybeBackup(t *testing.T) {
	_, q := schedulerDB(t)
	ctx := context.Background()
	backupTriedAt.Store(0)
	t.Cleanup(func() { backupTriedAt.Store(0) })
	loc := time.FixedZone("AEDT", 11*60*60)
	at := func(day, hour, minute int) time.Time { return time.Date(2026, 11, day, hour, minute, 0, 0, loc) }
	up := &fakeUploader{fail: errors.New("S3 is down")}
	try := func(now time.Time) {
		t.Helper()
		maybeBackup(ctx, q, up, now)
		if !jobs.Wait(5 * time.Second) {
			t.Fatal("backup still running")
		}
	}
	day := func() string { v, _ := q.GetMediaState(ctx, backupDayKey); return v }

	maybeBackup(ctx, q, nil, at(20, 3, 0)) // backups off: nothing happens
	try(at(20, 2, 59))
	if up.count() != 0 {
		t.Fatalf("backed up before 3 am")
	}
	try(at(20, 3, 0))
	if up.count() != 1 || day() != "" {
		t.Fatalf("after a failed backup: %d tries, day %q; want 1 and none", up.count(), day())
	}
	try(at(20, 3, 30))
	if up.count() != 1 {
		t.Errorf("tried again %d times within the hour", up.count()-1)
	}
	up.fail = nil
	try(at(20, 4, 1))
	if up.count() != 2 || day() != "2026-11-20" {
		t.Fatalf("after the retry: %d tries, day %q; want 2 and 2026-11-20", up.count(), day())
	}
	try(at(20, 5, 30))
	if up.count() != 2 {
		t.Errorf("backed up again the same day")
	}
	try(at(21, 3, 0))
	if up.count() != 3 || up.calls[2] != "2026-11-21" || day() != "2026-11-21" {
		t.Errorf("next day: calls %v, day %q", up.calls, day())
	}
}

// TestNewsSlot: news runs on the first tick in each 4-hour window, so a
// late tick still runs it, and only once.
func TestNewsSlot(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-19 04:00": "2026-11-19 04",
		"2026-11-19 04:07": "2026-11-19 04",
		"2026-11-19 04:30": "",
		"2026-11-19 05:00": "",
		"2026-11-19 00:01": "2026-11-19 00",
	} {
		tm, _ := time.Parse("2006-01-02 15:04", in)
		if got := newsSlotAt(tm); got != want {
			t.Errorf("newsSlotAt(%s) = %q, want %q", in, got, want)
		}
	}
}

// TestPanickingStepDoesntStopRun: a step of the scheduler's minute that
// panics (here the game emails, given no mailer while there's a
// notification to send) is logged, and the steps after it still run, on
// this tick and the next.
func TestPanickingStepDoesntStopRun(t *testing.T) {
	t.Setenv("TWELVEDATA_SYMBOLS", "TEST")
	d, q := schedulerDB(t)
	exec(t, d,
		`INSERT INTO users (id, email, name, email_token) VALUES (1, 'a@x.com', 'Alice', 't1')`,
	)
	// 3 am UTC on a Sunday: no Twelve Data jobs are due.
	now := time.Date(2026, 11, 22, 3, 1, 0, 0, time.UTC)
	for i := range 2 {
		exec(t, d,
			`INSERT INTO notifications (user_id, type, title, message) VALUES (1, 'order_filled', 'Filled', 'Bought 1 ACME')`,
			`INSERT INTO sessions (id, user_id, expires_at) VALUES ('s`+strconv.Itoa(i)+`', 1, '2000-01-01 00:00:00')`)
		run(context.Background(), q, d, time.UTC, nil, nil, now.Add(time.Duration(i)*time.Minute))
		if n := count(t, d, `SELECT COUNT(*) FROM sessions`); n != 0 {
			t.Fatalf("tick %d: the step after the panic didn't run (%d expired sessions left)", i+1, n)
		}
	}
	jobs.Wait(5 * time.Second)
}
