package scheduler

import (
	"testing"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/market"
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

// The load-test hook revalues without new prices, so production must never
// turn it on, whatever its .env says.
func TestEnableLoadTestRevalueRefusedInProduction(t *testing.T) {
	t.Cleanup(func() { loadTestRevalue.Store(false) })
	if EnableLoadTestRevalue(true) || loadTestRevalue.Load() {
		t.Fatal("load-test revaluation turned on in production")
	}
	if !EnableLoadTestRevalue(false) || !loadTestRevalue.Load() {
		t.Fatal("load-test revaluation didn't turn on outside production")
	}
}

// TestSplitsCheckDue: two checks each New York weekday, from 10:00 UTC and
// from 8:30 am in New York, each once it has gone through.
func TestSplitsCheckDue(t *testing.T) {
	if market.NewYork == nil {
		t.Skip("no America/New_York time zone")
	}
	utc := func(s string) time.Time {
		tm, err := time.Parse(time.DateTime, s)
		if err != nil {
			t.Fatal(err)
		}
		return tm
	}
	const mon = "2026-11-23" // EST: 8:30 am is 13:30 UTC
	for _, c := range []struct {
		name, now, done, early string
		late, due              bool
	}{
		{"before 10:00 UTC", "2026-11-23 09:59:00", "", "", false, false},
		{"early check", "2026-11-23 10:00:00", "", "", false, true},
		{"early done, waiting", "2026-11-23 13:29:00", "", mon, false, false},
		{"late check", "2026-11-23 13:30:00", "", mon, true, true},
		{"late done", "2026-11-23 20:00:00", mon, mon, false, false},
		{"started after 8:30 am: one late check", "2026-11-23 15:00:00", "", "", true, true},
		{"early done for an older day", "2026-11-23 10:05:00", "", "2026-11-20", false, true},
		{"Saturday", "2026-11-21 14:00:00", "", "", false, false},
		{"summer time: 8:30 am is 12:30 UTC", "2026-06-15 12:30:00", "", "2026-06-15", true, true},
	} {
		_, late, due := splitsCheckDue(utc(c.now), c.done, c.early)
		if late != c.late || due != c.due {
			t.Errorf("%s: late %v, due %v; want %v, %v", c.name, late, due, c.late, c.due)
		}
	}
}
