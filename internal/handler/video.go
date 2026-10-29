package handler

import (
	"net/url"
	"regexp"
	"strings"
)

// videoFrameSrc lists the players lessons can embed. The CSP's frame-src
// uses it, so a lesson can't embed anything else.
const videoFrameSrc = "https://www.youtube-nocookie.com https://player.vimeo.com https://iframe.mediadelivery.net"

var videoID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// embedURL turns a video link pasted by an admin into its player URL. It
// takes YouTube (watch, youtu.be, shorts, embed), Vimeo (including unlisted
// links with a hash) and Bunny Stream links, and reports false for anything
// else.
func embedURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	parts := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	ok := func(ids ...string) bool {
		for _, id := range ids {
			if !videoID.MatchString(id) {
				return false
			}
		}
		return true
	}

	switch host {
	case "youtube.com", "m.youtube.com", "youtube-nocookie.com":
		id := u.Query().Get("v")
		if len(parts) == 2 && (parts[0] == "embed" || parts[0] == "shorts" || parts[0] == "live") {
			id = parts[1]
		}
		if ok(id) {
			return "https://www.youtube-nocookie.com/embed/" + id + "?rel=0", true
		}
	case "youtu.be":
		if len(parts) == 1 && ok(parts[0]) {
			return "https://www.youtube-nocookie.com/embed/" + parts[0] + "?rel=0", true
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
		if len(parts) >= 1 && ok(parts[0]) && strings.Trim(parts[0], "0123456789") == "" {
			embed := "https://player.vimeo.com/video/" + parts[0]
			if hash != "" {
				if !ok(hash) {
					return "", false
				}
				embed += "?h=" + hash
			}
			return embed, true
		}
	case "iframe.mediadelivery.net", "player.mediadelivery.net":
		// Bunny Stream: /embed/{library}/{video} or /play/{library}/{video}
		if len(parts) == 3 && (parts[0] == "embed" || parts[0] == "play") && ok(parts[1], parts[2]) {
			return "https://iframe.mediadelivery.net/embed/" + parts[1] + "/" + parts[2], true
		}
	}
	return "", false
}
