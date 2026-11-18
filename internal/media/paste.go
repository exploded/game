package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
)

// Pasted is a video an admin adds by pasting its link: a YouTube video from
// another channel, or a stream recording.
type Pasted struct {
	ID        string // YouTube's id
	URL       string // the link as pasted
	Embed     string
	Vertical  bool
	Title     string // "" to look it up
	Published time.Time
}

// youTubeTitle asks YouTube's oEmbed for a video's title.
func youTubeTitle(ctx context.Context, link string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.youtube.com/oembed?format=json&url="+url.QueryEscape(link), nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oembed: %s", resp.Status)
	}
	var out struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Title, nil
}

// AddPasted stores a pasted YouTube video and returns its id. Its topics are
// picked on the next classifier run.
func (s *Service) AddPasted(ctx context.Context, p Pasted) (int64, error) {
	title := strings.TrimSpace(p.Title)
	if title == "" {
		t, err := youTubeTitle(ctx, p.URL)
		if err != nil {
			slog.Warn("oembed lookup", "video", p.ID, "error", err)
			return 0, errors.New("couldn't look up the video's title; type one in")
		}
		title = t
	}
	if title == "" {
		return 0, errors.New("type in a title for the video")
	}
	published := p.Published
	if published.IsZero() {
		published = time.Now()
	}
	return s.q.UpsertVideo(ctx, db.UpsertVideoParams{
		Platform:    "youtube",
		ExternalID:  p.ID,
		Url:         p.URL,
		EmbedUrl:    p.Embed,
		Title:       title,
		ThumbUrl:    "https://i.ytimg.com/vi/" + p.ID + "/mqdefault.jpg",
		PublishedAt: sqlTime(published),
		Vertical:    boolInt(p.Vertical),
	})
}
