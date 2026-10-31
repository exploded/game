package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
)

// TikTok LIVE has no API, so the site finds out when Rob goes live by asking
// the endpoint TikTok's own live page uses. It needs no login or key, but it
// isn't a public API: TikTok can change or block it without notice (it
// blocked a home connection after about 30 checks in 5 minutes). Every check
// is logged in tiktok_live_checks and shown on /admin/learn/live, and Go
// live now still works when checks fail.
type tiktokLive struct {
	base string
	http *http.Client
}

func newTikTokLive() *tiktokLive {
	return &tiktokLive{base: "https://www.tiktok.com", http: &http.Client{Timeout: 15 * time.Second}}
}

// tiktokAccount is Rob's TikTok account, which is also checked all day.
const tiktokAccount = "rob_goudie"

// The all-day check: every 5 minutes from 8 am to 7 pm (TIMEZONE), so a
// stream Rob didn't book still shows on the site. Around a booked stream,
// and while one is on, the check runs every minute.
const (
	allDayFrom  = 8
	allDayUntil = 19
	allDayEvery = 5
)

// tiktokRoomLive is the room status TikTok gives a stream on air (4 is
// offline).
const tiktokRoomLive = 2

// tiktokEndAfter is how many checks in a row must find Rob off air before
// his stream is ended, so a dropped connection doesn't end it.
const tiktokEndAfter = 5

// tiktokRoom is what TikTok says about an account's live room.
type tiktokRoom struct {
	live  bool
	title string // what Rob called the stream, often ""
}

// room asks TikTok whether user (rob_goudie) is live now. status is the
// HTTP status, 0 if TikTok couldn't be reached.
func (t *tiktokLive) room(ctx context.Context, user string) (room tiktokRoom, status int, err error) {
	u := t.base + "/api-live/user/room/?" + url.Values{
		"aid": {"1988"}, "uniqueId": {user}, "sourceType": {"54"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return room, 0, err
	}
	// With no user agent TikTok sends back nothing.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TheWealthLab)")
	req.Header.Set("Accept-Language", "en-AU,en;q=0.9")
	resp, err := t.http.Do(req)
	if err != nil {
		return room, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return room, resp.StatusCode, fmt.Errorf("TikTok answered %s", resp.Status)
	}
	var out struct {
		StatusCode int    `json:"statusCode"`
		Message    string `json:"message"`
		Data       struct {
			LiveRoom struct {
				Status int    `json:"status"`
				Title  string `json:"title"`
			} `json:"liveRoom"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return room, resp.StatusCode, fmt.Errorf("TikTok's reply isn't the JSON it used to send (it may be blocking the server): %w", err)
	}
	if out.StatusCode != 0 {
		return room, resp.StatusCode, fmt.Errorf("TikTok said %d %s", out.StatusCode, out.Message)
	}
	if out.Data.LiveRoom.Status == 0 {
		return room, resp.StatusCode, errors.New("TikTok's reply has no live status")
	}
	return tiktokRoom{
		live:  out.Data.LiveRoom.Status == tiktokRoomLive,
		title: strings.Join(strings.Fields(out.Data.LiveRoom.Title), " "),
	}, resp.StatusCode, nil
}

// tiktokUser is the account in a TikTok link (rob_goudie in
// https://www.tiktok.com/@rob_goudie/live), or "".
func tiktokUser(link string) string {
	u, err := url.Parse(link)
	if err != nil || !(u.Host == "tiktok.com" || strings.HasSuffix(u.Host, ".tiktok.com")) {
		return ""
	}
	first, _, _ := strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	if !strings.HasPrefix(first, "@") || len(first) < 2 {
		return ""
	}
	return first[1:]
}

// checkTikTokLive checks the accounts with a TikTok stream booked soon or on
// now, and Rob's account when the all-day check is due.
func (s *Service) checkTikTokLive(ctx context.Context, now time.Time) error {
	local := now.In(s.loc)
	if local.Hour() == 3 && local.Minute() == 0 {
		if err := s.q.PruneTikTokLiveChecks(ctx); err != nil {
			slog.Error("prune tiktok live checks", "error", err)
		}
	}
	events, err := s.q.ListTikTokLiveToCheck(ctx)
	if err != nil {
		return err
	}
	byUser := map[string][]db.LiveEvent{}
	for _, e := range events {
		if user := tiktokUser(e.Url); user != "" {
			byUser[user] = append(byUser[user], e)
		}
	}
	if _, booked := byUser[tiktokAccount]; !booked && s.allDayDue(ctx, local) {
		byUser[tiktokAccount] = nil
	}
	var errs []error
	for user, evs := range byUser {
		if err := s.checkTikTokAccount(ctx, user, evs); err != nil {
			errs = append(errs, fmt.Errorf("tiktok live check @%s: %w", user, err))
		}
	}
	return errors.Join(errs...)
}

// allDayDue says whether the all-day check of Rob's account is due at local:
// every 5 minutes from 8 am to 7 pm, unless TikTok refused a check today.
func (s *Service) allDayDue(ctx context.Context, local time.Time) bool {
	if local.Hour() < allDayFrom || local.Hour() >= allDayUntil || local.Minute()%allDayEvery != 0 {
		return false
	}
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	blocked, err := s.q.TikTokBlockedSince(ctx, db.TikTokBlockedSinceParams{Account: tiktokAccount, CheckedAt: sqlTime(midnight)})
	if err != nil {
		slog.Error("tiktok blocked check", "error", err)
		return false
	}
	return !blocked
}

// checkTikTokAccount asks whether user is live, logs the answer, and starts
// or ends their streams (events, nil for the all-day check).
func (s *Service) checkTikTokAccount(ctx context.Context, user string, events []db.LiveEvent) error {
	room, status, err := s.ttLive.room(ctx, user)
	result, detail := "offline", ""
	switch {
	case err != nil:
		result, detail = "error", err.Error()
	case room.live:
		result = "live"
	}
	id, lerr := s.q.LogTikTokLiveCheck(ctx, db.LogTikTokLiveCheckParams{
		Account: user, Booked: boolInt(events != nil), Result: result, HttpStatus: int64(status), Detail: detail,
	})
	if lerr != nil {
		return lerr
	}
	if err != nil {
		return err
	}
	note := func(eventID int64, what string) error {
		return s.q.NoteTikTokLiveCheck(ctx, db.NoteTikTokLiveCheckParams{Detail: what, EventID: nullInt(eventID), ID: id})
	}
	if room.live {
		return s.tiktokOnAir(ctx, user, events, room, note)
	}
	return s.tiktokOffAir(ctx, user, events, note)
}

// tiktokOnAir handles finding user live: a stream already on is noted as
// seen, otherwise the booked one nearest to now starts (its reminders go out
// on this minute's notifyLive), otherwise one is added.
func (s *Service) tiktokOnAir(ctx context.Context, user string, events []db.LiveEvent, room tiktokRoom, note func(int64, string) error) error {
	var next *db.LiveEvent
	for i, e := range events {
		if e.Status == "live" {
			return note(e.ID, "")
		}
		if next == nil || closerToNow(e, *next) {
			next = &events[i]
		}
	}
	if next != nil {
		if err := s.q.GoLive(ctx, next.ID); err != nil {
			return err
		}
		slog.Info("tiktok stream started by itself", "event", next.ID)
		return note(next.ID, "Started the booked stream.")
	}

	// Rob went live without booking a stream: add one so the banner shows.
	// Nobody could ask for a reminder, so no email goes out.
	title := room.title
	if title == "" {
		title = "Rob is live on TikTok"
	}
	if r := []rune(title); len(r) > 150 {
		title = string(r[:150])
	}
	id, err := s.q.CreateLiveEvent(ctx, db.CreateLiveEventParams{
		Platform: "tiktok", Url: "https://www.tiktok.com/@" + user + "/live", Title: title,
		ScheduledAt: nullTime(time.Now()), Status: "upcoming",
	})
	if err != nil {
		return err
	}
	if err := s.q.MarkLiveAnnounced(ctx, id); err != nil {
		return err
	}
	if err := s.q.GoLive(ctx, id); err != nil {
		return err
	}
	slog.Info("tiktok stream added by itself", "event", id, "title", title)
	return note(id, "Rob went live without a booked stream, so one was added.")
}

// tiktokOffAir ends user's streams once enough checks in a row have found
// them off air. A stream no check has seen on air (started by hand before
// Rob went live) is left alone.
func (s *Service) tiktokOffAir(ctx context.Context, user string, events []db.LiveEvent, note func(int64, string) error) error {
	for _, e := range events {
		if e.Status != "live" {
			continue
		}
		seen, err := s.q.TikTokStreamSeen(ctx, nullInt(e.ID))
		if err != nil {
			return err
		}
		if !seen {
			continue
		}
		streak, err := s.q.TikTokOfflineStreak(ctx, user)
		if err != nil {
			return err
		}
		if streak < tiktokEndAfter {
			continue
		}
		if err := s.q.EndLive(ctx, e.ID); err != nil {
			return err
		}
		slog.Info("tiktok stream ended by itself", "event", e.ID)
		if err := note(e.ID, fmt.Sprintf("Ended the stream after %d checks off air.", streak)); err != nil {
			return err
		}
	}
	return nil
}

// closerToNow says whether a is booked nearer to now than b.
func closerToNow(a, b db.LiveEvent) bool {
	gap := func(e db.LiveEvent) float64 {
		return math.Abs(time.Since(parseSQLTime(e.ScheduledAt.String)).Seconds())
	}
	return gap(a) < gap(b)
}
