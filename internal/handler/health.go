package handler

import (
	"context"
	"net/http"
	"time"
)

// Health is the uptime monitor's check. It's public and outside the auth
// groups: a check that needs a session can't tell "down" from "logged out".
// It pings the DB so a live process on a broken database reads as down.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	w.Header().Set("Cache-Control", "no-store")
	if err := h.rawDB.PingContext(ctx); err != nil {
		http.Error(w, "db unhealthy", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("ok"))
}
