package handler

import (
	"net/http"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// Dashboard shows this quarter's game and the next one, to open or join,
// and what's new from Rob.
func (h *Handler) Dashboard(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	extra := map[string]any{}
	if videos, err := h.q.ListLatestVideos(r.Context(), db.ListLatestVideosParams{Vertical: 0, Lim: 4}); err == nil {
		extra["RobVideos"] = videos
	}
	if upcoming, err := h.q.ListUpcomingLive(r.Context()); err == nil && len(upcoming) > 0 {
		extra["NextLive"] = upcoming[0]
	}
	h.render(w, r, "dashboard/index", "", PageData{
		Title: "Dashboard",
		Items: h.quarterCards(r.Context(), user.ID),
		Extra: extra,
	})
}
