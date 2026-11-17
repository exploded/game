package handler

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/exploded/game/internal/db"
)

// TikTok's embedded player can be switched off at /admin/learn. On 17
// November 2026 it began answering every video, Rob's and TikTok's own alike,
// with "Player authentication failed" (its /player/api/v1/items call gets a
// 403), on any network and in any browser. While it's off, the "player"
// template shows a TikTok video's cover as a link to the video on TikTok, so
// players never see TikTok's error. YouTube and Vimeo are untouched.

// tiktokPlayerKey is the media_state row holding the switch: "on" shows
// TikTok's player. Anything else, including no row, keeps it off, so a
// fresh database doesn't show a broken player.
const tiktokPlayerKey = "tiktok_player"

// tiktokPlayerOn mirrors the media_state row, so templates needn't query it.
var tiktokPlayerOn atomic.Bool

// loadTikTokPlayer reads the switch at startup.
func loadTikTokPlayer(ctx context.Context, q *db.Queries) {
	if q == nil { // template tests make a Handler with no database
		return
	}
	v, _ := q.GetMediaState(ctx, tiktokPlayerKey)
	tiktokPlayerOn.Store(v == "on")
}

// playerOff reports whether embed is TikTok's player while it's switched off.
func playerOff(embed string) bool {
	return !tiktokPlayerOn.Load() && strings.HasPrefix(embed, "https://www.tiktok.com/")
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
		setFlashCookie(w, "TikTok's player is on. Open a TikTok video on Learn to check it plays.", "success")
	} else {
		setFlashCookie(w, "TikTok's player is off. TikTok videos show their cover and a Watch on TikTok link.", "success")
	}
	http.Redirect(w, r, "/admin/learn", http.StatusSeeOther)
}
