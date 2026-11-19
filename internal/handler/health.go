package handler

import (
	"context"
	"net/http"
	"time"
)

// heartbeatStale is how long the scheduler can go without ticking before
// /health reports down. A tick normally comes every minute, but one can run
// for a few minutes (the daily price fetch waits for API credits), so this
// leaves room before monitor raises an alarm.
const heartbeatStale = 10 * time.Minute

// heartbeatSource is the scheduler's heartbeat, if one is wired in.
type heartbeatSource struct {
	last  func() time.Time // the last tick; zero before the first
	since time.Time        // when it was wired in, standing in for a tick until the first
}

// SetHeartbeat has /health report down when the scheduler's loop stops
// ticking. last returns the time of its latest tick.
func (h *Handler) SetHeartbeat(last func() time.Time) {
	h.heartbeat = heartbeatSource{last: last, since: time.Now()}
}

// stale says whether the scheduler hasn't ticked for heartbeatStale. A
// fresh start counts as a tick, so the deploy script's check right after
// a restart passes.
func (b heartbeatSource) stale(now time.Time) bool {
	if b.last == nil {
		return false
	}
	last := b.last()
	if last.Before(b.since) {
		last = b.since
	}
	return now.Sub(last) > heartbeatStale
}

// Health is the uptime monitor's check. It's public and outside the auth
// groups: a check that needs a session can't tell "down" from "logged out".
// It pings the DB so a live process on a broken database reads as down, and
// checks the scheduler is still ticking, since a stopped scheduler (no
// prices, orders, game changes or email) would otherwise go unnoticed.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	w.Header().Set("Cache-Control", "no-store")
	if err := h.rawDB.PingContext(ctx); err != nil {
		http.Error(w, "db unhealthy", http.StatusServiceUnavailable)
		return
	}
	if h.heartbeat.stale(time.Now()) {
		http.Error(w, "scheduler stopped", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.Write([]byte("ok"))
}
