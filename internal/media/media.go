// Package media keeps the Learn hub's videos and live streams up to date. It
// syncs Consortium's YouTube channel (Shorts included), asks Claude which
// topics each video belongs to, spots Rob going live on TikTok, and emails
// people about live streams. TikTok's videos aren't synced: each is also a
// YouTube Short, and TikTok's player wouldn't play in many browsers. The scheduler
// runs it every minute; each job is skipped when its keys aren't set.
package media

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/jobs"
	"github.com/exploded/game/internal/mail"
)

// Service runs the media jobs.
type Service struct {
	q      *db.Queries
	mailer *mail.Mailer
	loc    *time.Location

	yt       *YouTube // nil without YOUTUBE_API_KEY
	ttLive   *tiktokLive
	classify Classifier // nil without ANTHROPIC_API_KEY

	ytRunning       atomic.Bool
	ttLiveRunning   atomic.Bool
	classifyRunning atomic.Bool

	// Jobs that run once a day in a window, not on an exact minute: a
	// scheduler tick can be late, or the job before can still be running.
	ytFullSlot jobs.Slot // the full YouTube sync, from 17:30 UTC
	pruneSlot  jobs.Slot // pruning old TikTok checks, from 3 am (TIMEZONE)
}

// New reads the media settings from the environment. loc is the time zone
// emails show stream times in.
func New(q *db.Queries, m *mail.Mailer, loc *time.Location) *Service {
	s := &Service{q: q, mailer: m, loc: loc, ttLive: newTikTokLive()}
	if key := os.Getenv("YOUTUBE_API_KEY"); key != "" {
		channel := os.Getenv("YOUTUBE_CHANNEL")
		if channel == "" {
			channel = "@ConsortiumPW"
		}
		s.yt = NewYouTube(key, channel)
	} else {
		slog.Info("YouTube sync off: set YOUTUBE_API_KEY")
	}
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		s.classify = NewClaude(os.Getenv("CLAUDE_MODEL"))
	} else {
		slog.Info("video topics off: set ANTHROPIC_API_KEY")
	}
	return s
}

// Status says which jobs are configured, for the admin page.
type Status struct {
	YouTube, Claude bool
}

// Status reports which jobs have their keys.
func (s *Service) Status() Status {
	if s == nil {
		return Status{}
	}
	return Status{YouTube: s.yt != nil, Claude: s.classify != nil}
}

// Run does whatever is due at now. The syncs and the classifier run in the
// background so a slow API doesn't hold up the scheduler.
func (s *Service) Run(ctx context.Context, now time.Time) {
	if s == nil {
		return
	}
	utc := now.UTC()
	if s.yt != nil {
		fullSlot := ""
		if utc.Hour() == 17 && utc.Minute() >= 30 {
			fullSlot = utc.Format(time.DateOnly)
		}
		switch {
		case !s.done(ctx, stateYouTubeBackfilled) || s.ytFullSlot.Due(fullSlot):
			// The first run, and once a day (from 3:30 am in Sydney), reads
			// the whole channel to pick up edits and deletions.
			if s.background(&s.ytRunning, "youtube full sync", func() error { return s.syncYouTube(ctx, true) }) && fullSlot != "" {
				s.ytFullSlot.Done(fullSlot)
			}
		case utc.Minute()%10 == 0:
			s.background(&s.ytRunning, "youtube sync", func() error { return s.syncYouTube(ctx, false) })
		case utc.Minute()%2 == 0:
			s.background(&s.ytRunning, "youtube live check", func() error { return s.checkYouTubeLive(ctx) })
		}
	}
	s.background(&s.ttLiveRunning, "tiktok live check", func() error { return s.checkTikTokLive(ctx, now) })
	if s.classify != nil {
		s.background(&s.classifyRunning, "classify videos", func() error { return s.classifyPending(ctx) })
	}
	s.notifyLive(ctx, now)
	s.mailOnAirAnswers(ctx)
}

// SyncNow starts the YouTube sync (a full one when full is true) without waiting for the schedule.
func (s *Service) SyncNow(ctx context.Context, full bool) {
	if s == nil {
		return
	}
	if s.yt != nil {
		s.background(&s.ytRunning, "youtube sync", func() error { return s.syncYouTube(ctx, full) })
	}
}

// background runs job in a goroutine unless the previous run is still
// going, and reports whether it started it. A panic in job is logged and
// doesn't stop later runs.
func (s *Service) background(running *atomic.Bool, name string, job func() error) bool {
	if !running.CompareAndSwap(false, true) {
		return false
	}
	jobs.Go(name, func() {
		defer running.Store(false)
		if err := job(); err != nil {
			slog.Error(name, "error", err)
		}
	})
	return true
}

const (
	stateYouTubeUploads    = "youtube_uploads_playlist"
	stateYouTubeBackfilled = "youtube_backfilled"
)

func (s *Service) state(ctx context.Context, key string) string {
	v, err := s.q.GetMediaState(ctx, key)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Error("media state", "key", key, "error", err)
	}
	return v
}

func (s *Service) done(ctx context.Context, key string) bool { return s.state(ctx, key) == "1" }

func (s *Service) setState(ctx context.Context, key, value string) {
	if err := s.q.SetMediaState(ctx, db.SetMediaStateParams{Key: key, Value: value}); err != nil {
		slog.Error("save media state", "key", key, "error", err)
	}
}

// sqlTime formats t as SQLite's datetime('now') does, in UTC.
func sqlTime(t time.Time) string { return t.UTC().Format(time.DateTime) }

func nullTime(t time.Time) sql.NullString {
	if t.IsZero() {
		return sql.NullString{}
	}
	return sql.NullString{String: sqlTime(t), Valid: true}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
