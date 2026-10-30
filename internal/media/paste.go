package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
)

// Pasted is a video an admin adds by pasting its link: Facebook and
// Instagram videos (no API without Rob's Meta login), single TikToks before
// TikTok is connected, or a YouTube video from another channel.
type Pasted struct {
	Platform  string
	ID        string // the platform's id
	URL       string // the link as pasted (after following a short link)
	Embed     string
	Vertical  bool
	Title     string // "" to look it up (YouTube and TikTok)
	Published time.Time
}

// shortLinkHosts hide the video id behind a redirect.
var shortLinkHosts = map[string]bool{"vm.tiktok.com": true, "vt.tiktok.com": true, "fb.watch": true}

// ExpandShortLink follows a vm.tiktok.com or fb.watch link to the full
// video link. Other links come back as they are.
func ExpandShortLink(ctx context.Context, raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !shortLinkHosts[strings.ToLower(u.Hostname())] {
		return raw
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return raw
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; StockGame)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return raw
	}
	resp.Body.Close()
	return resp.Request.URL.String()
}

// oEmbed asks YouTube or TikTok for a video's title and cover.
func oEmbed(ctx context.Context, platform, link string) (title, thumb string, err error) {
	var endpoint string
	switch platform {
	case "youtube":
		endpoint = "https://www.youtube.com/oembed?format=json&url="
	case "tiktok":
		endpoint = "https://www.tiktok.com/oembed?url="
	default:
		return "", "", errors.New("no oEmbed for " + platform)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+url.QueryEscape(link), nil)
	if err != nil {
		return "", "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("oembed: %s", resp.Status)
	}
	var out struct {
		Title        string `json:"title"`
		ThumbnailURL string `json:"thumbnail_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", "", err
	}
	return out.Title, out.ThumbnailURL, nil
}

// AddPasted stores a pasted video and returns its id. Its topics are picked
// on the next classifier run.
func (s *Service) AddPasted(ctx context.Context, p Pasted) (int64, error) {
	title := strings.TrimSpace(p.Title)
	var cover string
	if title == "" || p.Platform == "tiktok" {
		t, c, err := oEmbed(ctx, p.Platform, p.URL)
		if err != nil && title == "" {
			return 0, errors.New("couldn't look up the video's title; type one in")
		}
		if title == "" {
			title = t
		}
		cover = c
	}
	if title == "" {
		return 0, errors.New("type in a title for the video")
	}
	thumbURL := ""
	if p.Platform == "youtube" {
		thumbURL = "https://i.ytimg.com/vi/" + p.ID + "/mqdefault.jpg"
	}
	published := p.Published
	if published.IsZero() {
		published = time.Now()
	}
	id, err := s.q.UpsertVideo(ctx, db.UpsertVideoParams{
		Platform:    p.Platform,
		ExternalID:  p.ID,
		Url:         p.URL,
		EmbedUrl:    p.Embed,
		Title:       title,
		ThumbUrl:    thumbURL,
		PublishedAt: sqlTime(published),
		Vertical:    boolInt(p.Vertical),
	})
	if err != nil {
		return 0, err
	}
	if cover != "" && thumbURL == "" {
		// TikTok's cover links expire, so keep a copy.
		_ = s.cacheThumb(ctx, id, cover)
	}
	return id, nil
}
