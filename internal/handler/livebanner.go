package handler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// liveBannerTTL is how long /live/banner reuses its answer. Every open page
// asks once a minute, so the database sees one query per 15 seconds however
// many players are on, and a stream shows up within about 75 seconds of the
// scheduler marking it live.
const liveBannerTTL = 15 * time.Second

// liveBannerCache holds /live/banner's last answer. The same banner goes to
// everyone, signed in or not, so one copy serves all.
type liveBannerCache struct {
	mu   sync.Mutex
	at   time.Time
	body []byte
	etag string
}

// LiveBanner sends the "Rob is live" banner, or nothing when he isn't, for
// app.js to put in the page's #live-slot. It's public, uncapped by the
// throttle and unlogged, since every open tab asks once a minute. An
// unchanged answer is a 304 with no body.
func (h *Handler) LiveBanner(w http.ResponseWriter, r *http.Request) {
	body, etag, err := h.liveBanner(r)
	if err != nil {
		slog.Error("live banner", "error", err)
		http.Error(w, "live banner unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", etag)
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}

// liveBanner renders the banner, at most once per liveBannerTTL. Callers
// wait on the lock while one of them refreshes it, so a crowd arriving at
// once runs one query, not one each.
func (h *Handler) liveBanner(r *http.Request) ([]byte, string, error) {
	c := &h.liveCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.etag != "" && time.Since(c.at) < liveBannerTTL {
		return c.body, c.etag, nil
	}
	live, err := h.q.ListLiveNow(r.Context())
	if err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	if len(live) > 0 {
		// Every page's template set has the partials; any one will do.
		tmpl, ok := h.pages["learn/live"]
		if !ok {
			return nil, "", errNoBannerTemplate
		}
		if err := tmpl.ExecuteTemplate(&buf, "live-banner", live[0]); err != nil {
			return nil, "", err
		}
	}
	sum := sha256.Sum256(buf.Bytes())
	c.body = buf.Bytes()
	c.etag = `W/"` + hex.EncodeToString(sum[:8]) + `"`
	c.at = time.Now()
	return c.body, c.etag, nil
}

var errNoBannerTemplate = errors.New("no learn/live template to draw the banner with")
