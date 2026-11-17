package handler

import (
	"encoding/xml"
	"fmt"
	"net/http"
)

// The sitemap and robots.txt tell search engines which public pages exist.
// Everything else needs an account, so it isn't listed: Learn too, since 17
// November 2026, when Rob asked for it to need a sign-in.

// sitemapPages are the public pages. /login is the landing page for visitors.
var sitemapPages = []string{"/login", "/about", "/help", "/terms", "/privacy"}

type sitemapURL struct {
	Loc string `xml:"loc"`
}

type sitemapSet struct {
	XMLName xml.Name     `xml:"urlset"`
	Xmlns   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

// Sitemap lists the public pages.
func (h *Handler) Sitemap(w http.ResponseWriter, r *http.Request) {
	base := siteURL(r)
	urls := make([]sitemapURL, len(sitemapPages))
	for i, p := range sitemapPages {
		urls[i] = sitemapURL{Loc: base + p}
	}
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write([]byte(xml.Header))
	xml.NewEncoder(w).Encode(sitemapSet{Xmlns: "http://www.sitemaps.org/schemas/sitemap/0.9", URLs: urls})
}

// IndexNowKey proves to IndexNow (indexnow.org: Bing, Yandex and others, not
// Google) that a URL list posted to api.indexnow.org comes from this site.
// It isn't a secret: it can only submit URLs on the host that serves it.
const IndexNowKey = "a03d77b6c8d8255b961049fb2a8aa755"

// IndexNowKeyFile serves the key at /{key}.txt, where IndexNow checks it.
func (h *Handler) IndexNowKeyFile(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(IndexNowKey))
}

// Robots keeps crawlers out of the signed-in pages and points them at the
// sitemap, which needs the site's full URL.
func (h *Handler) Robots(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	fmt.Fprintf(w, "User-agent: *\nDisallow: /admin/\nDisallow: /games/\n\nSitemap: %s/sitemap.xml\n", siteURL(r))
}
