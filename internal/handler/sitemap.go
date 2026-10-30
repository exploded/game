package handler

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/exploded/game/internal/db"
)

// The sitemap and robots.txt tell search engines which public pages exist.
// Everything else needs an account, so it isn't listed.

// sitemapPages are the public pages that aren't one of Rob's videos, topics
// or questions. /login is the landing page for visitors.
var sitemapPages = []string{
	"/login", "/learn", "/learn/live", "/learn/questions", "/learn/courses",
	"/about", "/help", "/contact", "/terms", "/privacy",
}

// sitemapVideos caps how many videos of each shape (wide and vertical) are
// listed. A sitemap holds up to 50,000 URLs.
const sitemapVideos = 5000

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// Sitemap lists the public pages, Learn topics with videos, visible videos
// and answered questions.
func (h *Handler) Sitemap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	base := siteURL(r)
	var urls []sitemapURL
	add := func(path, lastmod string) {
		urls = append(urls, sitemapURL{Loc: base + path, LastMod: lastmod})
	}
	for _, p := range sitemapPages {
		add(p, "")
	}

	topics, err := h.q.ListTopics(ctx)
	if err != nil {
		h.serverError(w, "sitemap topics", err)
		return
	}
	for _, t := range topics {
		if t.Videos > 0 {
			add("/learn/topics/"+url.PathEscape(t.Slug), "")
		}
	}
	for _, vertical := range []int64{0, 1} {
		videos, err := h.q.ListLatestVideos(ctx, db.ListLatestVideosParams{Vertical: vertical, Lim: sitemapVideos})
		if err != nil {
			h.serverError(w, "sitemap videos", err)
			return
		}
		for _, v := range videos {
			add(fmt.Sprintf("/learn/v/%d", v.ID), sitemapDate(v.PublishedAt))
		}
	}
	questions, err := h.q.ListQuestions(ctx, db.ListQuestionsParams{Show: "answered"})
	if err != nil {
		h.serverError(w, "sitemap questions", err)
		return
	}
	for _, q := range questions {
		add(fmt.Sprintf("/learn/questions/%d", q.ID), sitemapDate(q.AnsweredAt.String))
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(sitemapSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: urls})
}

// sitemapDate turns a stored time into the sitemap's date, or "" if it
// can't be read.
func sitemapDate(s string) string {
	for _, layout := range []string{time.DateTime, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format(time.DateOnly)
		}
	}
	return ""
}

// Robots keeps crawlers out of the signed-in pages and points them at the
// sitemap, which needs the site's full URL.
func (h *Handler) Robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /admin/\nDisallow: /games/\n\nSitemap: %s/sitemap.xml\n", siteURL(r))
}
