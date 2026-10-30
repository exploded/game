package media

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
)

// TikTok reads Rob's TikTok videos through TikTok's Display API. Rob connects
// his account once from /admin/learn (OAuth, Login Kit for Web); the access
// token lasts a day and is refreshed from the refresh token, which lasts a
// year. Scraping with his login instead would break TikTok's terms and risk
// the account.
type TikTok struct {
	key, secret string
	auth        string // the consent page
	api         string
	http        *http.Client
}

// NewTikTok returns a client for the app with this client key and secret.
func NewTikTok(key, secret string) *TikTok {
	return &TikTok{
		key:    key,
		secret: secret,
		auth:   "https://www.tiktok.com/v2/auth/authorize/",
		api:    "https://open.tiktokapis.com/v2",
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// tiktokScopes are what the sync needs: the account name and its videos.
const tiktokScopes = "user.info.basic,video.list"

// AuthURL is the TikTok consent page. TikTok sends Rob back to redirect with
// a code and state.
func (t *TikTok) AuthURL(redirect, state string) string {
	return t.auth + "?" + url.Values{
		"client_key":    {t.key},
		"scope":         {tiktokScopes},
		"response_type": {"code"},
		"redirect_uri":  {redirect},
		"state":         {state},
	}.Encode()
}

type tiktokToken struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int64  `json:"refresh_expires_in"`
	OpenID           string `json:"open_id"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func (t *TikTok) token(ctx context.Context, form url.Values) (tiktokToken, error) {
	form.Set("client_key", t.key)
	form.Set("client_secret", t.secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.api+"/oauth/token/", strings.NewReader(form.Encode()))
	if err != nil {
		return tiktokToken{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := t.http.Do(req)
	if err != nil {
		return tiktokToken{}, err
	}
	defer resp.Body.Close()
	var tok tiktokToken
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return tok, fmt.Errorf("tiktok token: %s: %w", resp.Status, err)
	}
	if tok.Error != "" || tok.AccessToken == "" {
		return tok, fmt.Errorf("tiktok token: %s %s", tok.Error, tok.ErrorDescription)
	}
	return tok, nil
}

// apiError is the error object every Display API response carries.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e apiError) err(what string) error {
	if e.Code == "" || e.Code == "ok" {
		return nil
	}
	return fmt.Errorf("tiktok %s: %s: %s", what, e.Code, e.Message)
}

// displayName asks TikTok for the connected account's name.
func (t *TikTok) displayName(ctx context.Context, access string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, t.api+"/user/info/?fields=open_id,display_name", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := t.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			User struct {
				DisplayName string `json:"display_name"`
			} `json:"user"`
		} `json:"data"`
		Error apiError `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Data.User.DisplayName, out.Error.err("user info")
}

// ttVideo is a Display API video object.
type ttVideo struct {
	ID               string `json:"id"`
	CreateTime       int64  `json:"create_time"`
	CoverImageURL    string `json:"cover_image_url"`
	ShareURL         string `json:"share_url"`
	VideoDescription string `json:"video_description"`
	Duration         int64  `json:"duration"`
	Height           int64  `json:"height"`
	Width            int64  `json:"width"`
	Title            string `json:"title"`
	ViewCount        int64  `json:"view_count"`
}

const tiktokVideoFields = "id,create_time,cover_image_url,share_url,video_description,duration,height,width,title,view_count"

// videoPage returns up to 20 videos posted before cursor (0 = newest), the
// cursor for the next page, and whether there is one.
func (t *TikTok) videoPage(ctx context.Context, access string, cursor int64) ([]ttVideo, int64, bool, error) {
	body := map[string]int64{"max_count": 20}
	if cursor != 0 {
		body["cursor"] = cursor
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.api+"/video/list/?fields="+tiktokVideoFields, bytes.NewReader(b))
	if err != nil {
		return nil, 0, false, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.http.Do(req)
	if err != nil {
		return nil, 0, false, err
	}
	defer resp.Body.Close()
	var out struct {
		Data struct {
			Videos  []ttVideo `json:"videos"`
			Cursor  int64     `json:"cursor"`
			HasMore bool      `json:"has_more"`
		} `json:"data"`
		Error apiError `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, 0, false, fmt.Errorf("tiktok video list: %s: %w", resp.Status, err)
	}
	if err := out.Error.err("video list"); err != nil {
		return nil, 0, false, err
	}
	return out.Data.Videos, out.Data.Cursor, out.Data.HasMore, nil
}

// TikTokEmbedURL is TikTok's embed player for a video id.
func TikTokEmbedURL(id string) string {
	return "https://www.tiktok.com/player/v1/" + id + "?rel=0&description=1"
}

// ConnectTikTok swaps the code from TikTok's consent page for tokens and
// stores them.
func (s *Service) ConnectTikTok(ctx context.Context, code, redirect string) error {
	if s.tt == nil {
		return errors.New("set TIKTOK_CLIENT_KEY and TIKTOK_CLIENT_SECRET first")
	}
	tok, err := s.tt.token(ctx, url.Values{
		"code":         {code},
		"grant_type":   {"authorization_code"},
		"redirect_uri": {redirect},
	})
	if err != nil {
		return err
	}
	if err := s.saveTikTokToken(ctx, tok, ""); err != nil {
		return err
	}
	s.background(&s.ttRunning, "tiktok sync", func() error { return s.syncTikTok(context.WithoutCancel(ctx)) })
	return nil
}

func (s *Service) saveTikTokToken(ctx context.Context, tok tiktokToken, account string) error {
	if account == "" {
		account, _ = s.tt.displayName(ctx, tok.AccessToken)
	}
	now := time.Now()
	return s.q.SaveOAuthToken(ctx, db.SaveOAuthTokenParams{
		Provider:         "tiktok",
		Account:          account,
		AccessToken:      tok.AccessToken,
		RefreshToken:     tok.RefreshToken,
		AccessExpiresAt:  sqlTime(now.Add(time.Duration(tok.ExpiresIn) * time.Second)),
		RefreshExpiresAt: sqlTime(now.Add(time.Duration(tok.RefreshExpiresIn) * time.Second)),
	})
}

// tiktokAccess returns a current access token, refreshing it if it expires
// within the hour.
func (s *Service) tiktokAccess(ctx context.Context) (string, error) {
	stored, err := s.q.GetOAuthToken(ctx, "tiktok")
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil // not connected yet
	}
	if err != nil {
		return "", err
	}
	if time.Until(parseSQLTime(stored.AccessExpiresAt)) > time.Hour {
		return stored.AccessToken, nil
	}
	if time.Until(parseSQLTime(stored.RefreshExpiresAt)) <= 0 {
		return "", errors.New("the TikTok connection has expired; connect TikTok again on /admin/learn")
	}
	tok, err := s.tt.token(ctx, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {stored.RefreshToken},
	})
	if err != nil {
		return "", err
	}
	if err := s.saveTikTokToken(ctx, tok, stored.Account); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

// syncTikTok stores Rob's TikTok videos. The first sync reads them all;
// later ones stop at the first page of videos already stored.
func (s *Service) syncTikTok(ctx context.Context) error {
	access, err := s.tiktokAccess(ctx)
	if err != nil || access == "" {
		return err
	}
	var cursor int64
	for page := 0; page < 200; page++ {
		videos, next, more, err := s.tt.videoPage(ctx, access, cursor)
		if err != nil {
			return err
		}
		known := 0
		for _, v := range videos {
			isNew, err := s.saveTikTokVideo(ctx, v)
			if err != nil {
				return err
			}
			if !isNew {
				known++
			}
		}
		if !more || (len(videos) > 0 && known == len(videos)) {
			break
		}
		cursor = next
	}
	return nil
}

// saveTikTokVideo stores a video and caches its cover, whose link expires
// after six hours. It reports whether the video is new.
func (s *Service) saveTikTokVideo(ctx context.Context, v ttVideo) (bool, error) {
	_, err := s.q.GetVideoByExternalID(ctx, db.GetVideoByExternalIDParams{Platform: "tiktok", ExternalID: v.ID})
	isNew := errors.Is(err, sql.ErrNoRows)

	title, desc := v.Title, v.VideoDescription
	if title == "" {
		title = firstLine(desc)
	}
	watch := v.ShareURL
	if watch == "" {
		watch = "https://www.tiktok.com/@rob_goudie/video/" + v.ID
	}
	id, err := s.q.UpsertVideo(ctx, db.UpsertVideoParams{
		Platform:        "tiktok",
		ExternalID:      v.ID,
		Url:             watch,
		EmbedUrl:        TikTokEmbedURL(v.ID),
		Title:           title,
		Description:     desc,
		PublishedAt:     sqlTime(time.Unix(v.CreateTime, 0)),
		DurationSeconds: v.Duration,
		Vertical:        boolInt(v.Height >= v.Width),
		ViewCount:       v.ViewCount,
	})
	if err != nil {
		return false, fmt.Errorf("save tiktok video %s: %w", v.ID, err)
	}
	if v.CoverImageURL != "" {
		if _, err := s.q.GetVideoThumb(ctx, id); errors.Is(err, sql.ErrNoRows) {
			if err := s.cacheThumb(ctx, id, v.CoverImageURL); err != nil {
				slog.Warn("cache tiktok cover", "video", v.ID, "error", err)
			}
		}
	}
	return isNew, nil
}

// maxThumb is the largest cover the cache keeps.
const maxThumb = 2 << 20

// cacheThumb downloads a cover image into video_thumbs.
func (s *Service) cacheThumb(ctx context.Context, videoID int64, src string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cover: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxThumb+1))
	if err != nil {
		return err
	}
	if len(data) > maxThumb {
		return errors.New("cover too large")
	}
	ct := http.DetectContentType(data)
	if !strings.HasPrefix(ct, "image/") {
		return fmt.Errorf("cover is %s, not an image", ct)
	}
	return s.q.UpsertVideoThumb(ctx, db.UpsertVideoThumbParams{VideoID: videoID, ContentType: ct, Data: data})
}

// firstLine is the start of a description, for a video with no title.
func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	if r := []rune(s); len(r) > 100 {
		return string(r[:100]) + "…"
	}
	if s == "" {
		return "TikTok video"
	}
	return s
}
