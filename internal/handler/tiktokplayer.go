package handler

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/exploded/game/internal/db"
)

// TikTok's embedded player works in some browsers and not others. From 17
// November 2026 it answers "Player authentication failed" (its
// /player/api/v1/items call gets a 403) in Chrome on a PC and Safari on an
// iPhone, yet plays in Firefox and in Chrome on an iPhone, probably because
// its bot check reads the browser's fingerprint. Which browsers pass can't be
// told from here, so the page finds out: watchTikTok in app.js keeps the
// player hidden until it reports it's ready and swaps in a link to the video
// on TikTok if it reports an error. An admin can still switch the player off
// at /admin/learn, and then every TikTok video is that link straight away.
// YouTube and Vimeo are untouched.

// tiktokPlayerKey is the media_state row holding the switch: "off" shows
// the link instead of TikTok's player. Anything else, including no row,
// leaves the player on.
const tiktokPlayerKey = "tiktok_player"

// tiktokPlayerOn mirrors the media_state row, so templates needn't query it.
var tiktokPlayerOn atomic.Bool

func init() { tiktokPlayerOn.Store(true) }

// loadTikTokPlayer reads the switch at startup.
func loadTikTokPlayer(ctx context.Context, q *db.Queries) {
	if q == nil { // template tests make a Handler with no database
		return
	}
	v, _ := q.GetMediaState(ctx, tiktokPlayerKey)
	tiktokPlayerOn.Store(v != "off")
}

// isTikTok reports whether embed is TikTok's player.
func isTikTok(embed string) bool {
	return strings.HasPrefix(embed, "https://www.tiktok.com/")
}

// playerOff reports whether embed is TikTok's player while it's switched off.
func playerOff(embed string) bool {
	return !tiktokPlayerOn.Load() && isTikTok(embed)
}

// AdminTikTokPlayer turns TikTok's embedded player on or off.
func (h *Handler) AdminTikTokPlayer(w http.ResponseWriter, r *http.Request) {
	on := r.FormValue("on") == "1"
	value := "off"
	if on {
		value = "on"
	}
	if err := h.q.SetMediaState(r.Context(), db.SetMediaStateParams{Key: tiktokPlayerKey, Value: value}); err != nil {
		h.serverError(w, "save tiktok player switch", err)
		return
	}
	tiktokPlayerOn.Store(on)
	if on {
		setFlashCookie(w, "TikTok's player is on. Where it can't play, TikTok videos still link to TikTok.", "success")
	} else {
		setFlashCookie(w, "TikTok's player is off. TikTok videos show their cover and a Watch on TikTok link.", "success")
	}
	http.Redirect(w, r, "/admin/learn", http.StatusSeeOther)
}
