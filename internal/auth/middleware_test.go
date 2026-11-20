package auth

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/exploded/game"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/stats"
	_ "modernc.org/sqlite"
)

// A player's first request of the day counts them active today, and the
// first of the month active this month, once each, with nothing stored about
// which days they came.
func TestTouchLastSeenCountsActivePlayers(t *testing.T) {
	loc, err := time.LoadLocation("Australia/Melbourne")
	if err != nil {
		t.Fatal(err)
	}
	stats.SetLocation(loc)
	t.Cleanup(func() { stats.SetLocation(time.UTC) })
	d, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	d.SetMaxOpenConns(1)
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(game.Schema); err != nil {
		t.Fatal(err)
	}
	q := db.New(d)
	ctx := context.Background()
	stats.Flush(ctx, d) // start from nothing held

	// active flushes the counts and returns this run's day and month totals.
	var days, months int64
	active := func() (int64, int64) {
		t.Helper()
		if err := stats.Flush(ctx, d); err != nil {
			t.Fatal(err)
		}
		var day, month int64
		d.QueryRow("SELECT COALESCE(SUM(n), 0) FROM stat_counts WHERE metric = ?", stats.ActiveDay).Scan(&day)
		d.QueryRow("SELECT COALESCE(SUM(n), 0) FROM stat_counts WHERE metric = ?", stats.ActiveMonth).Scan(&month)
		gotDay, gotMonth := day-days, month-months
		days, months = day, month
		return gotDay, gotMonth
	}
	user := func(id int64, lastSeen string) *db.User {
		t.Helper()
		seen := sql.NullString{String: lastSeen, Valid: lastSeen != ""}
		if _, err := d.Exec("INSERT INTO users (id, email, name, email_token, last_seen) VALUES (?, ?, 'P', ?, ?)",
			id, id, id, seen); err != nil {
			t.Fatal(err)
		}
		return &db.User{ID: id, LastSeen: seen}
	}
	utc := func(s string) time.Time {
		v, err := time.Parse(time.DateTime, s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	tests := []struct {
		name           string
		lastSeen, now  string // UTC
		wantDay, wantM int64
	}{
		// 23:50 on 19 November in Melbourne (UTC+11), then 00:10 on the 20th:
		// under an hour apart, but a new day.
		{"new day under an hour later", "2026-11-19 12:50:00", "2026-11-19 13:10:00", 1, 0},
		// 23:50 on 31 October, then 00:10 on 1 November: a new month too.
		{"new month", "2026-10-31 12:50:00", "2026-10-31 13:10:00", 1, 1},
		{"never seen before", "", "2026-11-19 13:10:00", 1, 1},
		// 08:00 then 14:00 the same day: touched, not counted.
		{"same day", "2026-11-19 21:00:00", "2026-11-20 03:00:00", 0, 0},
	}
	for i, tt := range tests {
		u := user(int64(i+1), tt.lastSeen)
		touchLastSeen(ctx, q, u, utc(tt.now))
		if day, month := active(); day != tt.wantDay || month != tt.wantM {
			t.Errorf("%s: counted %d today and %d this month, want %d and %d", tt.name, day, month, tt.wantDay, tt.wantM)
		}
	}

	// Two requests at once, both holding the old last_seen: counted once.
	u := user(10, "2026-11-19 12:50:00")
	again := *u
	touchLastSeen(ctx, q, u, utc("2026-11-19 13:10:00"))
	touchLastSeen(ctx, q, &again, utc("2026-11-19 13:10:00"))
	if day, _ := active(); day != 1 {
		t.Errorf("two requests at once counted %d, want 1", day)
	}
}
