package scheduler

import (
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
)

func TestDailyFetchDue(t *testing.T) {
	at := func(hhmm string) time.Time {
		tm, err := time.Parse(time.DateTime, "2026-10-29 "+hhmm+":00")
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	run := func(created string, ok int64) db.PriceFetchLog {
		return db.PriceFetchLog{Mode: "daily", CreatedAt: created, SymbolsOk: ok}
	}
	for _, c := range []struct {
		name  string
		last  db.PriceFetchLog
		found bool
		now   time.Time
		want  bool
	}{
		{"before 22:00", db.PriceFetchLog{}, false, at("21:59"), false},
		{"no daily run yet", db.PriceFetchLog{}, false, at("22:00"), true},
		// A backfill earlier today isn't a daily run, so GetLastDailyFetch
		// finds yesterday's: still due.
		{"last daily run yesterday", run("2026-10-28 22:01:30", 539), true, at("22:00"), true},
		{"done today", run("2026-10-29 22:01:30", 539), true, at("22:30"), false},
		{"failed 5 minutes ago", run("2026-10-29 22:01:00", 0), true, at("22:06"), false},
		{"failed 20 minutes ago", run("2026-10-29 22:01:00", 0), true, at("22:21"), true},
	} {
		if got := dailyFetchDue(c.last, c.found, c.now); got != c.want {
			t.Errorf("%s: dailyFetchDue = %v, want %v", c.name, got, c.want)
		}
	}
}
