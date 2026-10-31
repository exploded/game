package media

import (
	"context"
	"encoding/json"
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

// TikTok LIVE has no API, so the site finds out when Rob starts a booked
// TikTok stream by asking the endpoint TikTok's own live page uses. It needs
// no login or key, but it isn't a public API: TikTok can change or block it
// without notice. A failed check is logged and shown on /admin/learn/live,
// and Go live now still works.
type tiktokLive struct {
	base string
	http *http.Client

	// Only touched by checkTikTokLive, which never runs twice at once.
	seen    map[int64]bool // streams this check has seen on air
	offline map[int64]int  // checks in a row that found a seen stream off air
}

func newTikTokLive() *tiktokLive {
	return &tiktokLive{
		base:    "https://www.tiktok.com",
		http:    &http.Client{Timeout: 15 * time.Second},
		seen:    map[int64]bool{},
		offline: map[int64]int{},
	}
}

// tiktokRoomLive is the room status TikTok gives a stream on air (4 is
// offline).
const tiktokRoomLive = 2

// tiktokEndAfter is how many checks in a row (a minute apart) must find Rob
// off air before his stream is ended, so a dropped connection doesn't end it.
const tiktokEndAfter = 5

// isLive asks TikTok whether user (rob_goudie) is live now.
func (t *tiktokLive) isLive(ctx context.Context, user string) (bool, error) {
	u := t.base + "/api-live/user/room/?" + url.Values{
		"aid": {"1988"}, "uniqueId": {user}, "sourceType": {"54"},
	}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	// With no user agent TikTok sends back nothing.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TheWealthLab)")
	req.Header.Set("Accept-Language", "en-AU,en;q=0.9")
	resp, err := t.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("TikTok answered %s", resp.Status)
	}
	var out struct {
		StatusCode int    `json:"statusCode"`
		Message    string `json:"message"`
		Data       struct {
			LiveRoom struct {
				Status int `json:"status"`
			} `json:"liveRoom"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
		return false, fmt.Errorf("TikTok's reply isn't the JSON it used to send (it may be blocking the server): %w", err)
	}
	if out.StatusCode != 0 {
		return false, fmt.Errorf("TikTok said %d %s", out.StatusCode, out.Message)
	}
	if out.Data.LiveRoom.Status == 0 {
		return false, fmt.Errorf("TikTok's reply has no live status")
	}
	return out.Data.LiveRoom.Status == tiktokRoomLive, nil
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

const (
	stateTikTokLiveChecked = "tiktok_live_checked" // when the last check ran (UTC)
	stateTikTokLiveResult  = "tiktok_live_result"  // what it found
)

// checkTikTokLive starts a booked TikTok stream when Rob goes live, and ends
// one it saw start once he's been off air for five minutes.
func (s *Service) checkTikTokLive(ctx context.Context) error {
	events, err := s.q.ListTikTokLiveToCheck(ctx)
	if err != nil || len(events) == 0 {
		return err
	}
	byUser := map[string][]db.LiveEvent{}
	for _, e := range events {
		if user := tiktokUser(e.Url); user != "" {
			byUser[user] = append(byUser[user], e)
		}
	}
	for user, evs := range byUser {
		live, err := s.ttLive.isLive(ctx, user)
		result := "Rob wasn't live."
		switch {
		case err != nil:
			result = "Failed: " + err.Error()
		case live:
			result = "Rob was live."
		}
		s.setState(ctx, stateTikTokLiveChecked, sqlTime(time.Now()))
		s.setState(ctx, stateTikTokLiveResult, "@"+user+": "+result)
		if err != nil {
			return fmt.Errorf("tiktok live check @%s: %w", user, err)
		}
		if err := s.applyTikTokLive(ctx, evs, live); err != nil {
			return err
		}
	}
	return nil
}

// applyTikTokLive updates one account's streams from whether it's live.
func (s *Service) applyTikTokLive(ctx context.Context, events []db.LiveEvent, live bool) error {
	t := s.ttLive
	if live {
		var next *db.LiveEvent
		for i, e := range events {
			if e.Status == "live" {
				t.seen[e.ID] = true
				delete(t.offline, e.ID)
				next = nil
				break
			}
			if next == nil || closerToNow(e, *next) {
				next = &events[i]
			}
		}
		if next == nil {
			return nil
		}
		// Rob is live with no stream started: start the booked one nearest
		// to now. Its reminders go out on this minute's notifyLive.
		if err := s.q.GoLive(ctx, next.ID); err != nil {
			return err
		}
		t.seen[next.ID] = true
		slog.Info("tiktok stream started by itself", "event", next.ID)
		return nil
	}
	for _, e := range events {
		// A stream started by hand that this check never saw on air may
		// not have begun yet, so it's left alone.
		if e.Status != "live" || !t.seen[e.ID] {
			continue
		}
		t.offline[e.ID]++
		if t.offline[e.ID] < tiktokEndAfter {
			continue
		}
		if err := s.q.EndLive(ctx, e.ID); err != nil {
			return err
		}
		delete(t.seen, e.ID)
		delete(t.offline, e.ID)
		slog.Info("tiktok stream ended by itself", "event", e.ID)
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

// TikTokLiveCheck is when the TikTok live check last ran and what it found,
// for /admin/learn/live. at is zero when it has never run.
func (s *Service) TikTokLiveCheck(ctx context.Context) (at time.Time, result string) {
	if s == nil {
		return time.Time{}, ""
	}
	return parseSQLTime(s.state(ctx, stateTikTokLiveChecked)), s.state(ctx, stateTikTokLiveResult)
}
