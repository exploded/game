package stats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/exploded/game/internal/db"
)

// Metrics Rollup counts from the tables that already record them.
const (
	Signups       = "signups"
	Trades        = "trades"
	Traders       = "traders"      // players who traded that day
	FirstTrades   = "first_trades" // players' first trade in a game
	ChatPosted    = "chat_posted"
	ChatBlocked   = "chat_blocked"
	Questions     = "questions"
	QuestionVotes = "question_votes"
	LessonsDone   = "lessons_done" // lessons marked done
	Invites       = "invites"
	Referrals     = "referrals" // invite bonuses paid
)

// rollupDayKey is the media_state row holding the last day Rollup stored.
const rollupDayKey = "stats_day"

// LastRolledDay is the last day Rollup stored ("2006-01-02"), or "" before
// its first run.
func LastRolledDay(ctx context.Context, q *db.Queries) string {
	day, _ := q.GetMediaState(ctx, rollupDayKey)
	return day
}

// DayCounts counts what happened on day ("2006-01-02" in TIMEZONE) from the
// tables that record it. /admin/stats calls it for today, which Rollup
// hasn't stored yet.
func DayCounts(ctx context.Context, q *db.Queries, day string) (map[string]int64, error) {
	start, err := time.ParseInLocation(time.DateOnly, day, Location())
	if err != nil {
		return nil, err
	}
	// AddDate works in the day's own zone, so a day with a daylight saving
	// change is 23 or 25 hours long.
	end := start.AddDate(0, 0, 1)
	r, err := q.DayCounts(ctx, db.DayCountsParams{
		FromAt: start.UTC().Format(time.DateTime),
		ToAt:   end.UTC().Format(time.DateTime),
	})
	if err != nil {
		return nil, err
	}
	return map[string]int64{
		Signups: r.Signups, Trades: r.Trades, Traders: r.Traders, FirstTrades: r.FirstTrades,
		ChatPosted: r.ChatPosted, ChatBlocked: r.ChatBlocked,
		Questions: r.Questions, QuestionVotes: r.QuestionVotes,
		LessonsDone: r.LessonsDone, Invites: r.Invites, Referrals: r.Referrals,
	}, nil
}

// Rollup stores DayCounts for every finished day not stored yet: from the
// first sign-up on a new database, otherwise from the day after the last one
// stored, up to yesterday. Each day is stored once and never recounted, so a
// later deletion (an anonymised account, a deleted lobby post) doesn't change
// the past. now is the current time.
func Rollup(ctx context.Context, rawDB *sql.DB, now time.Time) error {
	q := db.New(rawDB)
	last, err := q.GetMediaState(ctx, rollupDayKey)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var day time.Time
	if last == "" {
		first, err := q.FirstSignupAt(ctx)
		if err != nil {
			return err
		}
		if first == "" {
			return nil // no users yet
		}
		t, err := time.ParseInLocation(time.DateTime, first, time.UTC)
		if err != nil {
			return fmt.Errorf("first sign-up %q: %w", first, err)
		}
		day = DayStart(t)
	} else {
		t, err := time.ParseInLocation(time.DateOnly, last, Location())
		if err != nil {
			return fmt.Errorf("%s %q: %w", rollupDayKey, last, err)
		}
		day = t.AddDate(0, 0, 1)
	}
	today := DayStart(now)
	for day.Before(today) {
		if err := storeDay(ctx, rawDB, day.Format(time.DateOnly)); err != nil {
			return err
		}
		day = day.AddDate(0, 0, 1)
	}
	return nil
}

// storeDay stores one day's counts and records it as done, together.
func storeDay(ctx context.Context, rawDB *sql.DB, day string) error {
	counts, err := DayCounts(ctx, db.New(rawDB), day)
	if err != nil {
		return fmt.Errorf("count %s: %w", day, err)
	}
	tx, err := rawDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := db.New(tx)
	for metric, n := range counts {
		if n == 0 {
			continue
		}
		if err := q.SetStatCount(ctx, db.SetStatCountParams{Period: day, Metric: metric, N: n}); err != nil {
			return err
		}
	}
	if err := q.SetMediaState(ctx, db.SetMediaStateParams{Key: rollupDayKey, Value: day}); err != nil {
		return err
	}
	return tx.Commit()
}
