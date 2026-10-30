package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/exploded/game/internal/db"
)

// YouTube reads a channel through the YouTube Data API v3. Each list call
// costs 1 unit of the 10,000-a-day quota: a full sync of 1,600 videos is
// about 64, and the 10-minute and 2-minute checks are 2 each.
type YouTube struct {
	key     string
	channel string // a handle (@ConsortiumPW) or a channel id (UC...)
	base    string
	shorts  string // where Shorts live, to tell them from other short videos
	http    *http.Client
}

// NewYouTube returns a client for channel.
func NewYouTube(key, channel string) *YouTube {
	return &YouTube{
		key:     key,
		channel: channel,
		base:    "https://www.googleapis.com/youtube/v3",
		shorts:  "https://www.youtube.com/shorts/",
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// ytVideo is the part of a videos.list item the sync uses.
type ytVideo struct {
	ID      string `json:"id"`
	Snippet struct {
		PublishedAt          time.Time `json:"publishedAt"`
		Title                string    `json:"title"`
		Description          string    `json:"description"`
		LiveBroadcastContent string    `json:"liveBroadcastContent"` // "none" | "upcoming" | "live"
		Thumbnails           map[string]struct {
			URL string `json:"url"`
		} `json:"thumbnails"`
	} `json:"snippet"`
	ContentDetails struct {
		Duration string `json:"duration"` // ISO 8601, e.g. PT12M5S
	} `json:"contentDetails"`
	Statistics struct {
		ViewCount string `json:"viewCount"`
	} `json:"statistics"`
	LiveStreamingDetails *struct {
		ScheduledStartTime time.Time `json:"scheduledStartTime"`
		ActualStartTime    time.Time `json:"actualStartTime"`
		ActualEndTime      time.Time `json:"actualEndTime"`
	} `json:"liveStreamingDetails"`
}

func (y *YouTube) get(ctx context.Context, path string, params url.Values, out any) error {
	params.Set("key", y.key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, y.base+path+"?"+params.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := y.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("youtube %s: %s: %s", path, resp.Status, body)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// uploadsPlaylist finds the channel's uploads playlist.
func (y *YouTube) uploadsPlaylist(ctx context.Context) (string, error) {
	params := url.Values{"part": {"contentDetails"}}
	if strings.HasPrefix(y.channel, "UC") {
		params.Set("id", y.channel)
	} else {
		params.Set("forHandle", strings.TrimPrefix(y.channel, "@"))
	}
	var out struct {
		Items []struct {
			ContentDetails struct {
				RelatedPlaylists struct {
					Uploads string `json:"uploads"`
				} `json:"relatedPlaylists"`
			} `json:"contentDetails"`
		} `json:"items"`
	}
	if err := y.get(ctx, "/channels", params, &out); err != nil {
		return "", err
	}
	if len(out.Items) == 0 || out.Items[0].ContentDetails.RelatedPlaylists.Uploads == "" {
		return "", fmt.Errorf("youtube channel %s not found", y.channel)
	}
	return out.Items[0].ContentDetails.RelatedPlaylists.Uploads, nil
}

// playlistPage returns up to 50 video ids from a playlist, newest first, and
// the token for the next page ("" on the last).
func (y *YouTube) playlistPage(ctx context.Context, playlist, pageToken string) ([]string, string, error) {
	params := url.Values{"part": {"contentDetails"}, "playlistId": {playlist}, "maxResults": {"50"}}
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}
	var out struct {
		NextPageToken string `json:"nextPageToken"`
		Items         []struct {
			ContentDetails struct {
				VideoID string `json:"videoId"`
			} `json:"contentDetails"`
		} `json:"items"`
	}
	if err := y.get(ctx, "/playlistItems", params, &out); err != nil {
		return nil, "", err
	}
	ids := make([]string, 0, len(out.Items))
	for _, it := range out.Items {
		ids = append(ids, it.ContentDetails.VideoID)
	}
	return ids, out.NextPageToken, nil
}

// videos returns the details of up to 50 videos. Private and deleted ones
// are left out.
func (y *YouTube) videos(ctx context.Context, ids []string) ([]ytVideo, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var out struct {
		Items []ytVideo `json:"items"`
	}
	err := y.get(ctx, "/videos", url.Values{
		"part": {"snippet,contentDetails,statistics,liveStreamingDetails"},
		"id":   {strings.Join(ids, ",")},
	}, &out)
	return out.Items, err
}

// isShort asks youtube.com whether a video is a Short: /shorts/{id} answers
// 200 for a Short and redirects to /watch for anything else. The API has no
// field for it. It reports ok=false when it couldn't tell.
func (y *YouTube) isShort(ctx context.Context, id string) (short, ok bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, y.shorts+id, nil)
	if err != nil {
		return false, false
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, false
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return true, true
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return false, true
	}
	return false, false
}

// syncYouTube reads the channel's uploads: every page when full is true,
// otherwise the newest 50. A full sync also removes videos that are no longer
// on the channel.
func (s *Service) syncYouTube(ctx context.Context, full bool) error {
	playlist := s.state(ctx, stateYouTubeUploads)
	if playlist == "" {
		var err error
		if playlist, err = s.yt.uploadsPlaylist(ctx); err != nil {
			return err
		}
		s.setState(ctx, stateYouTubeUploads, playlist)
	}

	seen := map[string]bool{}
	token := ""
	for page := 0; ; page++ {
		ids, next, err := s.yt.playlistPage(ctx, playlist, token)
		if err != nil {
			return err
		}
		if _, err := s.saveYouTube(ctx, ids); err != nil {
			return err
		}
		for _, id := range ids {
			seen[id] = true
		}
		if !full || next == "" || page > 200 {
			break
		}
		if page%10 == 9 {
			slog.Info("youtube full sync", "videos", len(seen))
		}
		token = next
	}
	if err := s.checkYouTubeLive(ctx); err != nil {
		return err
	}
	if full {
		s.removeGoneYouTube(ctx, seen)
		s.setState(ctx, stateYouTubeBackfilled, "1")
		slog.Info("youtube full sync done", "videos", len(seen))
	}
	return nil
}

// checkYouTubeLive re-reads the streams that are upcoming or live, so the
// Live tab changes within two minutes of Rob starting or ending one.
func (s *Service) checkYouTubeLive(ctx context.Context) error {
	ids, err := s.q.ListLiveYouTubeVideoIDs(ctx)
	if err != nil || len(ids) == 0 {
		return err
	}
	for start := 0; start < len(ids); start += 50 {
		batch := ids[start:min(start+50, len(ids))]
		found, err := s.saveYouTube(ctx, batch)
		if err != nil {
			return err
		}
		// A stream deleted or made private before it ran no longer comes
		// back from the API.
		for _, id := range batch {
			if !found[id] {
				if err := s.q.DeleteVideoByExternalID(ctx, db.DeleteVideoByExternalIDParams{Platform: "youtube", ExternalID: id}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// saveYouTube fetches the details of up to 50 videos and stores them. It
// returns the ids the API knew about.
func (s *Service) saveYouTube(ctx context.Context, ids []string) (map[string]bool, error) {
	items, err := s.yt.videos(ctx, ids)
	if err != nil {
		return nil, err
	}
	shorts := s.findShorts(ctx, items)
	found := make(map[string]bool, len(items))
	for _, v := range items {
		if err := s.saveYouTubeVideo(ctx, v, shorts[v.ID]); err != nil {
			return nil, err
		}
		found[v.ID] = true
	}
	return found, nil
}

// findShorts says which of the videos are Shorts. Only videos of 3 minutes
// or less can be; a stored answer is kept, and new ones are asked of
// youtube.com eight at a time, since each takes a round trip.
func (s *Service) findShorts(ctx context.Context, items []ytVideo) map[string]bool {
	shorts := map[string]bool{}
	var ask []ytVideo
	for _, v := range items {
		seconds := parseISODuration(v.ContentDetails.Duration)
		if v.Snippet.LiveBroadcastContent != "none" || seconds <= 0 || seconds > 180 {
			continue
		}
		existing, err := s.q.GetVideoByExternalID(ctx, db.GetVideoByExternalIDParams{Platform: "youtube", ExternalID: v.ID})
		if err == nil {
			shorts[v.ID] = existing.Vertical == 1
			continue
		}
		ask = append(ask, v)
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for _, v := range ask {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			short, ok := s.yt.isShort(ctx, v.ID)
			if !ok {
				short = parseISODuration(v.ContentDetails.Duration) <= 60
			}
			mu.Lock()
			shorts[v.ID] = short
			mu.Unlock()
		}()
	}
	wg.Wait()
	return shorts
}

func (s *Service) saveYouTubeVideo(ctx context.Context, v ytVideo, vertical bool) error {
	seconds := parseISODuration(v.ContentDetails.Duration)
	live := v.Snippet.LiveBroadcastContent
	if live == "none" {
		live = ""
	}
	watch := "https://www.youtube.com/watch?v=" + v.ID
	if vertical {
		watch = "https://www.youtube.com/shorts/" + v.ID
	}
	// The larger sizes are letterboxed 4:3; the cards crop them. A Short's
	// thumbnail is cropped to its middle, so it needs the biggest one.
	sizes := []string{"high", "medium", "default"}
	if vertical {
		sizes = []string{"standard", "high", "medium", "default"}
	}
	thumb := ""
	for _, size := range sizes {
		if t, ok := v.Snippet.Thumbnails[size]; ok && t.URL != "" {
			thumb = t.URL
			break
		}
	}
	views, _ := strconv.ParseInt(v.Statistics.ViewCount, 10, 64)
	published := v.Snippet.PublishedAt
	lsd := v.LiveStreamingDetails
	if lsd != nil && live == "upcoming" && !lsd.ScheduledStartTime.IsZero() {
		published = lsd.ScheduledStartTime
	}
	embed := YouTubeEmbedURL(v.ID)
	id, err := s.q.UpsertVideo(ctx, db.UpsertVideoParams{
		Platform:        "youtube",
		ExternalID:      v.ID,
		Url:             watch,
		EmbedUrl:        embed,
		Title:           v.Snippet.Title,
		Description:     v.Snippet.Description,
		ThumbUrl:        thumb,
		PublishedAt:     sqlTime(published),
		DurationSeconds: seconds,
		Vertical:        boolInt(vertical),
		LiveStatus:      live,
		WasLive:         boolInt(lsd != nil && !lsd.ActualStartTime.IsZero()),
		ViewCount:       views,
	})
	if err != nil {
		return fmt.Errorf("save youtube video %s: %w", v.ID, err)
	}
	if lsd == nil {
		return nil
	}
	status := "ended"
	switch live {
	case "upcoming":
		status = "upcoming"
	case "live":
		status = "live"
	}
	if status == "ended" && lsd.ActualStartTime.IsZero() {
		// A scheduled stream that never ran (or a premiere): nothing to list.
		return nil
	}
	return s.q.UpsertYouTubeLiveEvent(ctx, db.UpsertYouTubeLiveEventParams{
		Url:         "https://www.youtube.com/watch?v=" + v.ID,
		EmbedUrl:    embed,
		VideoID:     nullInt(id),
		Title:       v.Snippet.Title,
		Description: v.Snippet.Description,
		ScheduledAt: nullTime(lsd.ScheduledStartTime),
		StartedAt:   nullTime(lsd.ActualStartTime),
		EndedAt:     nullTime(lsd.ActualEndTime),
		Status:      status,
	})
}

// removeGoneYouTube deletes YouTube videos the full sync didn't see: Rob
// deleted them or made them private, so they'd no longer play. It does
// nothing if the sync saw fewer than 90% of the stored videos, in case the
// API returned a partial list.
func (s *Service) removeGoneYouTube(ctx context.Context, seen map[string]bool) {
	stored, err := s.q.ListPlatformVideoIDs(ctx, "youtube")
	if err != nil {
		slog.Error("list youtube videos", "error", err)
		return
	}
	if len(stored) == 0 || len(seen)*10 < len(stored)*9 {
		slog.Warn("youtube sync saw too few videos to remove any", "seen", len(seen), "stored", len(stored))
		return
	}
	removed := 0
	for _, v := range stored {
		if seen[v.ExternalID] {
			continue
		}
		if err := s.q.DeleteVideo(ctx, v.ID); err != nil {
			slog.Error("remove youtube video", "id", v.ID, "error", err)
			continue
		}
		removed++
	}
	if removed > 0 {
		slog.Info("removed youtube videos no longer on the channel", "count", removed)
	}
}

// YouTubeEmbedURL is the privacy-enhanced player for a video id. It matches
// what handler.embedURL builds from a pasted link.
func YouTubeEmbedURL(id string) string {
	return "https://www.youtube-nocookie.com/embed/" + id + "?rel=0"
}

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// parseISODuration turns "PT1H2M3S" into 3723. It returns 0 for anything it
// can't read, including a live stream's "P0D".
func parseISODuration(s string) int64 {
	m := isoDuration.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	var total int64
	for i, unit := range []int64{86400, 3600, 60, 1} {
		if m[i+1] != "" {
			n, _ := strconv.ParseInt(m[i+1], 10, 64)
			total += n * unit
		}
	}
	return total
}

func parseSQLTime(s string) time.Time {
	t, err := time.Parse(time.DateTime, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nullInt(v int64) sql.NullInt64 { return sql.NullInt64{Int64: v, Valid: true} }
