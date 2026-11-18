package handler

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/exploded/game/internal/media"
)

// videoFrameSrc lists the players lessons and Learn videos can embed. The
// CSP's frame-src uses it, so a page can't embed anything else.
const videoFrameSrc = "https://www.youtube-nocookie.com https://player.vimeo.com https://iframe.mediadelivery.net"

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// pastedVideo is a video link an admin pasted, worked out.
type pastedVideo struct {
	Platform string // youtube, vimeo or bunny
	ID       string // the platform's id for the video
	Embed    string // the player URL
	Vertical bool   // a YouTube Short (9:16)
}

// embedURL turns a video link pasted by an admin into its player URL. It
// reports false for anything parseVideo doesn't take.
func embedURL(raw string) (string, bool) {
	v, ok := parseVideo(raw)
	return v.Embed, ok
}

// parseVideo works out a pasted link. It takes YouTube (watch, youtu.be,
// shorts, live, embed), Vimeo (including unlisted links with a hash) and
// Bunny Stream links. Facebook and Instagram went on 17 November 2026, and
// TikTok on 18 November 2026: its player wouldn't play in many browsers.
func parseVideo(raw string) (pastedVideo, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return pastedVideo{}, false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	host = strings.TrimPrefix(host, "m.")
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	ok := func(ids ...string) bool {
		for _, id := range ids {
			if !videoID.MatchString(id) {
				return false
			}
		}
		return true
	}
	digits := func(s string) bool { return s != "" && strings.Trim(s, "0123456789") == "" }

	switch host {
	case "youtube.com", "youtube-nocookie.com":
		id := u.Query().Get("v")
		if len(parts) == 2 && (parts[0] == "embed" || parts[0] == "shorts" || parts[0] == "live") {
			id = parts[1]
		}
		if ok(id) {
			return pastedVideo{"youtube", id, media.YouTubeEmbedURL(id), len(parts) == 2 && parts[0] == "shorts"}, true
		}
	case "youtu.be":
		if len(parts) == 1 && ok(parts[0]) {
			return pastedVideo{"youtube", parts[0], media.YouTubeEmbedURL(parts[0]), false}, true
		}
	case "vimeo.com", "player.vimeo.com":
		// vimeo.com/123, vimeo.com/123/abcdef (unlisted),
		// player.vimeo.com/video/123?h=abcdef
		if len(parts) > 0 && parts[0] == "video" {
			parts = parts[1:]
		}
		hash := u.Query().Get("h")
		if len(parts) == 2 {
			hash = parts[1]
		}
		if len(parts) >= 1 && ok(parts[0]) && digits(parts[0]) {
			embed := "https://player.vimeo.com/video/" + parts[0]
			if hash != "" {
				if !ok(hash) {
					return pastedVideo{}, false
				}
				embed += "?h=" + hash
			}
			return pastedVideo{"vimeo", parts[0], embed, false}, true
		}
	case "iframe.mediadelivery.net", "player.mediadelivery.net":
		// Bunny Stream: /embed/{library}/{video} or /play/{library}/{video}
		if len(parts) == 3 && (parts[0] == "embed" || parts[0] == "play") && ok(parts[1], parts[2]) {
			return pastedVideo{"bunny", parts[2], "https://iframe.mediadelivery.net/embed/" + parts[1] + "/" + parts[2], false}, true
		}
	}
	return pastedVideo{}, false
}
