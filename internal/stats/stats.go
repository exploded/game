// Package stats counts how often things happen on the site, by day and month
// in TIMEZONE, for /admin/stats. It keeps counts only: nothing it stores says
// who did what, and it adds no rows per player (James's rule, 20 November
// 2026). Keep it that way: count with Add, never by user.
//
// Counts come from two places. Rollup (rollup.go) counts each finished day
// from tables that already record events, such as sign-ups, trades and chat.
// Add counts what nothing else records (video plays, page opens, a player's
// first visit of the day) in memory, and Flush writes those once a minute, so
// counting costs a request no database write.
package stats

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"time"

	"github.com/exploded/game/internal/db"
)

// Metrics counted with Add. The ones Rollup counts are in rollup.go.
const (
	ActiveDay      = "active_day"      // players' first signed-in request of the day
	ActiveMonth    = "active_month"    // and of the month (a month period)
	VideoPlay      = "video_play"      // a Learn video started; item is the video's id
	LivePlay       = "live_play"       // a live stream's own player started; item is the stream's id
	LessonPlay     = "lesson_play"     // a course lesson started; item is the lesson's id
	IntroPlay      = "intro_play"      // the site tour on the home and sign-in pages
	CourseView     = "course_view"     // a course's page opened; item is the course's id
	CalculatorView = "calculator_view" // a calculator opened; item is its slug
)

type key struct{ period, metric, item string }

var (
	mu      sync.Mutex
	pending = map[key]int64{}
	zone    atomic.Pointer[time.Location]

	// now is the clock; tests set it.
	now = time.Now
)

// SetLocation sets the time zone days and months are counted in (TIMEZONE).
func SetLocation(loc *time.Location) { zone.Store(loc) }

// Location is the time zone days and months are counted in, UTC until
// SetLocation is called.
func Location() *time.Location {
	if loc := zone.Load(); loc != nil {
		return loc
	}
	return time.UTC
}

// DayOf is t's day in TIMEZONE, as "2006-01-02".
func DayOf(t time.Time) string { return t.In(Location()).Format(time.DateOnly) }

// MonthOf is t's month in TIMEZONE, as "2006-01".
func MonthOf(t time.Time) string { return t.In(Location()).Format("2006-01") }

// DayStart is midnight at the start of t's day in TIMEZONE.
func DayStart(t time.Time) time.Time {
	l := t.In(Location())
	return time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, Location())
}

// MonthStart is midnight at the start of t's month in TIMEZONE.
func MonthStart(t time.Time) time.Time {
	l := t.In(Location())
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, Location())
}

// Now is the clock the counts use.
func Now() time.Time { return now() }

// Add counts one of metric today, for item ("" for the whole site).
func Add(metric, item string) { add(DayOf(now()), metric, item, 1) }

// AddMonth counts one of metric this month, for the counts that can't be
// summed from days (a player active on 20 days is one active player).
func AddMonth(metric string) { add(MonthOf(now()), metric, "", 1) }

func add(period, metric, item string, n int64) {
	mu.Lock()
	pending[key{period, metric, item}] += n
	mu.Unlock()
}

// Flush writes the counts held in memory, in one transaction. If that fails,
// the counts are kept for the next try. The scheduler calls it every minute
// and main once more at shutdown.
func Flush(ctx context.Context, rawDB *sql.DB) error {
	mu.Lock()
	batch := pending
	pending = map[key]int64{}
	mu.Unlock()
	if len(batch) == 0 {
		return nil
	}
	err := write(ctx, rawDB, batch)
	if err != nil {
		mu.Lock()
		for k, n := range batch {
			pending[k] += n
		}
		mu.Unlock()
	}
	return err
}

func write(ctx context.Context, rawDB *sql.DB, batch map[key]int64) error {
	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	for k, n := range batch {
		if err := q.AddStatCount(ctx, db.AddStatCountParams{Period: k.period, Metric: k.metric, Item: k.item, N: n}); err != nil {
			return err
		}
	}
	return tx.Commit()
}
