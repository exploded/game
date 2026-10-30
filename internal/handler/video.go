package handler

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/exploded/game/internal/media"
)

// videoFrameSrc lists the players lessons and Learn videos can embed. The
// CSP's frame-src uses it, so a page can't embed anything else.
const videoFrameSrc = "https://www.youtube-nocookie.com https://player.vimeo.com https://iframe.mediadelivery.net " +
	"https://www.tiktok.com https://www.facebook.com https://www.instagram.com"

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// pastedVideo is a video link an admin pasted, worked out.
type pastedVideo struct {
	Platform string // youtube, vimeo, bunny, tiktok, facebook or instagram
	ID       string // the platform's id for the video
	Embed    string // the player URL
	Vertical bool   // a short-form (9:16) link: a YouTube Short, TikTok or reel
}

// embedURL turns a video link pasted by an admin into its player URL. It
// reports false for anything parseVideo doesn't take.
func embedURL(raw string) (string, bool) {
	v, ok := parseVideo(raw)
	return v.Embed, ok
}

// parseVideo works out a pasted link. It takes YouTube (watch, youtu.be,
// shorts, live, embed), Vimeo (including unlisted links with a hash), Bunny
// Stream, TikTok, Facebook and Instagram video links. Short links that hide
// the id (vm.tiktok.com, fb.watch) have to be followed first.
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
	case "tiktok.com":
		// tiktok.com/@user/video/{id}, /player/v1/{id}, /embed/v2/{id}
		var id string
		switch {
		case len(parts) == 3 && strings.HasPrefix(parts[0], "@") && parts[1] == "video":
			id = parts[2]
		case len(parts) == 3 && parts[0] == "player" && parts[1] == "v1",
			len(parts) == 3 && parts[0] == "embed" && parts[1] == "v2":
			id = parts[2]
		}
		if digits(id) && ok(id) {
			return pastedVideo{"tiktok", id, media.TikTokEmbedURL(id), true}, true
		}
	case "facebook.com":
		// facebook.com/{page}/videos/{id}, /watch/?v={id}, /reel/{id}
		var id string
		reel := false
		switch {
		case len(parts) >= 1 && parts[0] == "watch":
			id = u.Query().Get("v")
		case len(parts) == 2 && parts[0] == "reel":
			id, reel = parts[1], true
		case len(parts) >= 3 && parts[1] == "videos":
			id = parts[len(parts)-1]
		}
		if digits(id) && ok(id) {
			href := "https://www.facebook.com/watch/?v=" + id
			if reel {
				href = "https://www.facebook.com/reel/" + id
			}
			embed := "https://www.facebook.com/plugins/video.php?" + url.Values{"href": {href}, "show_text": {"false"}}.Encode()
			return pastedVideo{"facebook", id, embed, reel}, true
		}
	case "instagram.com":
		// instagram.com/p/{code}/, /reel/{code}/, /reels/{code}/, /tv/{code}/
		if len(parts) >= 2 && ok(parts[1]) {
			switch parts[0] {
			case "p", "tv":
				return pastedVideo{"instagram", parts[1], "https://www.instagram.com/p/" + parts[1] + "/embed", false}, true
			case "reel", "reels":
				return pastedVideo{"instagram", parts[1], "https://www.instagram.com/reel/" + parts[1] + "/embed", true}, true
			}
		}
	}
	return pastedVideo{}, false
}
