package media

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// Live stream emails (docs/emails.md L1 and L2):
//   - live_scheduled goes to everyone with Live on when a stream is first
//     seen at least an hour before it starts.
//   - live_starting goes to the people who tapped "Remind me" when it starts.

// announceAhead is how far ahead a stream must be to be announced; one
// found later than that only gets the "starting" email.
const announceAhead = time.Hour

// startGrace is how late the "starting" email can go. A stream found after
// that (the server was down, or it was added after the fact) gets none.
const startGrace = 30 * time.Minute

// liveEmail is the data for the live_* templates.
type liveEmail struct {
	Title    string
	When     string // "Thu 6 Nov, 7:00 pm AEDT"
	Platform string // "YouTube"
	URL      string // the stream's page on StockGame
	WatchURL string // the stream on the platform
}

// PlatformName is how a platform is written in the UI.
func PlatformName(p string) string {
	switch p {
	case "youtube":
		return "YouTube"
	case "tiktok":
		return "TikTok"
	case "facebook":
		return "Facebook"
	case "instagram":
		return "Instagram"
	case "x":
		return "X"
	}
	return p
}

func (s *Service) liveEmail(e db.LiveEvent) liveEmail {
	d := liveEmail{
		Title:    e.Title,
		Platform: PlatformName(e.Platform),
		URL:      "/learn/live/" + strconv.FormatInt(e.ID, 10),
		WatchURL: e.Url,
	}
	if t := parseSQLTime(e.ScheduledAt.String); e.ScheduledAt.Valid && !t.IsZero() {
		d.When = t.In(s.loc).Format("Mon 2 Jan, 3:04 pm MST")
	}
	return d
}

// notifyLive queues the live stream emails that are due.
func (s *Service) notifyLive(ctx context.Context, now time.Time) {
	announce, err := s.q.ListLiveToAnnounce(ctx)
	if err != nil {
		slog.Error("list streams to announce", "error", err)
		return
	}
	for _, e := range announce {
		if err := s.q.MarkLiveAnnounced(ctx, e.ID); err != nil {
			slog.Error("mark stream announced", "event", e.ID, "error", err)
			return
		}
		start := parseSQLTime(e.ScheduledAt.String)
		if !e.ScheduledAt.Valid || start.Sub(now) < announceAhead {
			continue
		}
		ids, err := s.q.ListActiveUserIDs(ctx)
		if err != nil {
			slog.Error("list users for stream email", "error", err)
			return
		}
		n := s.mailEach(ctx, ids, "live_scheduled", s.liveEmail(e))
		slog.Info("announced live stream", "event", e.ID, "emails", n)
	}

	starting, err := s.q.ListLiveToNotifyStart(ctx)
	if err != nil {
		slog.Error("list streams starting", "error", err)
		return
	}
	for _, e := range starting {
		if err := s.q.MarkLiveStartNotified(ctx, e.ID); err != nil {
			slog.Error("mark stream start notified", "event", e.ID, "error", err)
			return
		}
		started := parseSQLTime(e.StartedAt.String)
		if e.StartedAt.Valid && now.Sub(started) > startGrace {
			continue
		}
		ids, err := s.q.ListLiveReminderUserIDs(ctx, e.ID)
		if err != nil {
			slog.Error("list stream reminders", "event", e.ID, "error", err)
			return
		}
		for _, id := range ids {
			if err := s.q.CreateNotification(ctx, db.CreateNotificationParams{
				UserID: id, Type: "live",
				Title:   "Rob is live now",
				Message: e.Title + " on " + PlatformName(e.Platform) + ".",
			}); err != nil {
				slog.Error("stream notification", "user", id, "error", err)
			}
		}
		n := s.mailEach(ctx, ids, "live_starting", s.liveEmail(e))
		slog.Info("stream started", "event", e.ID, "reminders", len(ids), "emails", n)
	}
}

// GoLive marks an admin-added stream live now, so the banner shows and the
// reminders go out on the next tick.
func (s *Service) GoLive(ctx context.Context, id int64) error {
	return s.q.GoLive(ctx, id)
}

func (s *Service) mailEach(ctx context.Context, ids []int64, template string, data any) int {
	n := 0
	for _, id := range ids {
		queued, err := s.mailer.ToUser(ctx, id, mail.Live, template, data)
		if err != nil {
			slog.Error("queue "+template, "user", id, "error", err)
		}
		if queued {
			n++
		}
	}
	return n
}

// onAirEmail is the data for question_answered.tmpl.
type onAirEmail struct {
	Title      string
	URL        string
	AnsweredBy string
	Asker      bool
	Answer     string
}

// mailOnAirAnswers emails the followers of questions Rob answered during a
// stream, once the stream is over (and, for YouTube, once its recording is
// in, so the email links to the moment).
func (s *Service) mailOnAirAnswers(ctx context.Context) {
	rows, err := s.q.ListOnAirAnswersToMail(ctx)
	if err != nil {
		slog.Error("list on-air answers", "error", err)
		return
	}
	for _, r := range rows {
		if err := s.q.MarkAnswerMailed(ctx, r.ID); err != nil {
			slog.Error("mark answer mailed", "question", r.ID, "error", err)
			return
		}
		if r.EventVideoID.Valid {
			if err := s.q.SetQuestionVideo(ctx, db.SetQuestionVideoParams{
				AnswerVideoID: r.EventVideoID, AnswerSeconds: r.AnswerSeconds, ID: r.ID,
			}); err != nil {
				slog.Error("link on-air answer", "question", r.ID, "error", err)
			}
		}
		followers, err := s.q.ListQuestionFollowers(ctx, r.ID)
		if err != nil {
			slog.Error("list question followers", "question", r.ID, "error", err)
			continue
		}
		by := r.AnsweredByName
		if by == "" {
			by = "Rob"
		}
		for _, id := range followers {
			if _, err := s.mailer.ToUser(ctx, id, mail.Community, "question_answered", onAirEmail{
				Title:      r.Title,
				URL:        "/learn/questions/" + strconv.FormatInt(r.ID, 10),
				AnsweredBy: by,
				Asker:      id == r.UserID,
				Answer:     r.Answer,
			}); err != nil {
				slog.Error("queue on-air answer email", "question", r.ID, "user", id, "error", err)
			}
		}
	}
}
