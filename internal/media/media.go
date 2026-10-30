// Package media keeps the Learn hub's videos and live streams up to date. It
// syncs Rob's YouTube channel and TikTok account, asks Claude which topics
// each video belongs to, and emails people about live streams. The scheduler
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
	"github.com/exploded/game/internal/mail"
)

// Service runs the media jobs.
type Service struct {
	q      *db.Queries
	mailer *mail.Mailer
	loc    *time.Location

	yt       *YouTube   // nil without YOUTUBE_API_KEY
	tt       *TikTok    // nil without TIKTOK_CLIENT_KEY and TIKTOK_CLIENT_SECRET
	classify Classifier // nil without ANTHROPIC_API_KEY

	ytRunning       atomic.Bool
	ttRunning       atomic.Bool
	classifyRunning atomic.Bool
}

// New reads the media settings from the environment. loc is the time zone
// emails show stream times in.
func New(q *db.Queries, m *mail.Mailer, loc *time.Location) *Service {
	s := &Service{q: q, mailer: m, loc: loc}
	if key := os.Getenv("YOUTUBE_API_KEY"); key != "" {
		channel := os.Getenv("YOUTUBE_CHANNEL")
		if channel == "" {
			channel = "@ConsortiumPW"
		}
		s.yt = NewYouTube(key, channel)
	} else {
		slog.Info("YouTube sync off: set YOUTUBE_API_KEY")
	}
	if key, secret := os.Getenv("TIKTOK_CLIENT_KEY"), os.Getenv("TIKTOK_CLIENT_SECRET"); key != "" && secret != "" {
		s.tt = NewTikTok(key, secret)
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
	YouTube, TikTok, Claude bool
}

// Status reports which jobs have their keys.
func (s *Service) Status() Status {
	if s == nil {
		return Status{}
	}
	return Status{YouTube: s.yt != nil, TikTok: s.tt != nil, Claude: s.classify != nil}
}

// TikTok returns the TikTok client, or nil when TikTok isn't configured.
func (s *Service) TikTok() *TikTok {
	if s == nil {
		return nil
	}
	return s.tt
}

// Run does whatever is due at now. The syncs and the classifier run in the
// background so a slow API doesn't hold up the scheduler.
func (s *Service) Run(ctx context.Context, now time.Time) {
	if s == nil {
		return
	}
	utc := now.UTC()
	if s.yt != nil {
		switch {
		case !s.done(ctx, stateYouTubeBackfilled) || (utc.Hour() == 17 && utc.Minute() == 30):
			// The first run, and once a day (3:30 am in Sydney), reads the
			// whole channel to pick up edits and deletions.
			s.background(&s.ytRunning, "youtube full sync", func() error { return s.syncYouTube(ctx, true) })
		case utc.Minute()%10 == 0:
			s.background(&s.ytRunning, "youtube sync", func() error { return s.syncYouTube(ctx, false) })
		case utc.Minute()%2 == 0:
			s.background(&s.ytRunning, "youtube live check", func() error { return s.checkYouTubeLive(ctx) })
		}
	}
	if s.tt != nil && utc.Minute() == 15 {
		s.background(&s.ttRunning, "tiktok sync", func() error { return s.syncTikTok(ctx) })
	}
	if s.classify != nil {
		s.background(&s.classifyRunning, "classify videos", func() error { return s.classifyPending(ctx) })
	}
	s.notifyLive(ctx, now)
	s.mailOnAirAnswers(ctx)
}

// SyncNow starts the YouTube and TikTok syncs (full YouTube sync when full
// is true) without waiting for the schedule.
func (s *Service) SyncNow(ctx context.Context, full bool) {
	if s == nil {
		return
	}
	if s.yt != nil {
		s.background(&s.ytRunning, "youtube sync", func() error { return s.syncYouTube(ctx, full) })
	}
	if s.tt != nil {
		s.background(&s.ttRunning, "tiktok sync", func() error { return s.syncTikTok(ctx) })
	}
}

// background runs job in a goroutine unless the previous run is still going.
func (s *Service) background(running *atomic.Bool, name string, job func() error) {
	if !running.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer running.Store(false)
		if err := job(); err != nil {
			slog.Error(name, "error", err)
		}
	}()
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
