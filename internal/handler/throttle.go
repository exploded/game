package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Throttle caps how many requests the app works on at once, for every user
// together (docs/capacity.md). Every request waits for the one SQLite
// connection, so past a handful at once more only queue. Without a cap an
// overload makes every page slower until players give up; with it, a request
// past the backlog, or one that waits too long for a place, gets a quick 503
// with Retry-After, and the rest stay fast.
//
// A request that gets a place also gets a deadline, so one stuck waiting for
// the database gives its place up rather than holding it after the server's
// WriteTimeout has already dropped the answer.
type Throttle struct {
	slots    chan struct{} // requests being worked on
	queue    chan struct{} // those plus the ones waiting for a place
	wait     time.Duration
	deadline time.Duration

	refused atomic.Int64 // since the last warning
	warned  atomic.Int64 // Unix seconds of the last warning
}

// NewThrottle works on limit requests at once and lets backlog more wait up
// to wait for a place. Each request's context ends after deadline.
func NewThrottle(limit, backlog int, wait, deadline time.Duration) *Throttle {
	return &Throttle{
		slots:    make(chan struct{}, limit),
		queue:    make(chan struct{}, limit+backlog),
		wait:     wait,
		deadline: deadline,
	}
}

// throttleExempt are paths the cap leaves alone: static files are cheap and
// cached, /health is the uptime check, and /mcp holds connections open for
// minutes (an AI app's listen stream) and has its own per-user limit.
func throttleExempt(path string) bool {
	return strings.HasPrefix(path, "/static/") || path == "/favicon.ico" || path == "/health" || path == "/mcp"
}

// Middleware applies the cap.
func (t *Throttle) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if throttleExempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		select {
		case t.queue <- struct{}{}:
		default:
			t.refuse(w, r) // the backlog is full
			return
		}
		defer func() { <-t.queue }()

		select {
		case t.slots <- struct{}{}:
		default:
			timer := time.NewTimer(t.wait)
			select {
			case t.slots <- struct{}{}:
				timer.Stop()
			case <-timer.C:
				t.refuse(w, r)
				return
			case <-r.Context().Done():
				timer.Stop()
				return // the player gave up
			}
		}
		defer func() { <-t.slots }()

		ctx, cancel := context.WithTimeout(r.Context(), t.deadline)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// busyPage is what a refused page load shows. It has no script and no
// template, so it costs nothing to send.
const busyPage = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Busy - The Investor Project</title></head>
<body style="font-family: system-ui, sans-serif; max-width: 32rem; margin: 15vh auto; padding: 0 16px; text-align: center; color: #1f2937; background: #fff">
<h1 style="font-size: 1.5rem">The Investor Project is busy</h1>
<p>Lots of players are on at once. Please try again in a minute.</p>
<p><a href="/dashboard">Try again</a></p>
</body></html>
`

// busyRetry is the Retry-After the refusal sends, in seconds.
const busyRetry = 30

func (t *Throttle) refuse(w http.ResponseWriter, r *http.Request) {
	// Warn at most once a minute, so an overload doesn't flood the logs
	// shipped to monitor.
	n := t.refused.Add(1)
	now := time.Now().Unix()
	if last := t.warned.Load(); now-last >= 60 && t.warned.CompareAndSwap(last, now) {
		n = t.refused.Swap(0)
		slog.Warn("server busy: requests refused", "refused", n, "path", r.URL.Path)
	}
	w.Header().Set("Retry-After", strconv.Itoa(busyRetry))
	w.Header().Set("Cache-Control", "no-store")
	if isHTMX(r) || r.Method != http.MethodGet || !strings.Contains(r.Header.Get("Accept"), "text/html") {
		http.Error(w, "The site is busy. Please try again in a minute.", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	io.WriteString(w, busyPage)
}
