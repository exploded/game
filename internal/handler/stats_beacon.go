package handler

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/game/internal/stats"
)

// StatsPlay counts a video starting to play (POST /stats/play, sent by
// app.js once per player per page). kind is "video" (a Learn video), "live"
// (a stream's own player), "lesson" or "intro" (the site tour), and id the
// video, stream or lesson. It only adds to a count for that video and day and
// never looks at who is watching, so it has no session or CSRF check, only
// the cross-origin check and rate limit in Routes.
func (h *Handler) StatsPlay(w http.ResponseWriter, r *http.Request) {
	kind := r.FormValue("kind")
	if kind == "intro" {
		stats.Add(stats.IntroPlay, "")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	id, err := strconv.ParseInt(r.FormValue("id"), 10, 64)
	if err != nil || id <= 0 {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	// Only real videos, streams and lessons are counted, so made-up ids
	// can't fill the counts table.
	var metric string
	switch kind {
	case "video":
		metric = stats.VideoPlay
		_, err = h.q.GetVideo(r.Context(), id)
	case "live":
		metric = stats.LivePlay
		_, err = h.q.GetLiveEvent(r.Context(), id)
	case "lesson":
		metric = stats.LessonPlay
		_, err = h.q.GetLesson(r.Context(), id)
	default:
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}
	stats.Add(metric, strconv.FormatInt(id, 10))
	w.WriteHeader(http.StatusNoContent)
}

// countCalculatorView counts each opening of a calculator's page
// (/tools/{slug}), by calculator and day. Only that it was opened: what
// anyone types into a calculator never reaches the server.
func countCalculatorView(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if slug, ok := strings.CutPrefix(r.URL.Path, "/tools/"); ok && slug != "" {
				stats.Add(stats.CalculatorView, slug)
			}
		}
		next.ServeHTTP(w, r)
	})
}
