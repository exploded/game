package market

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	_ "image/gif" // registers GIF with image.Decode
	_ "image/png" // registers PNG with image.Decode

	"github.com/exploded/game/internal/db"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp" // registers WebP with image.Decode
	"golang.org/x/net/html"
)

// A shown story gets a thumbnail from its article's og:image, the picture a
// site sets for link previews. TickerTick sends no picture. The image is
// copied, scaled down and re-encoded into news_thumbs, so the stock page
// serves it from our own site: the CSP stays as it is, players' addresses
// don't go to the news sites, and we only ever serve pixels we drew. Only the
// NewsShown stories the stock page shows get one, which keeps news_thumbs at
// about two per stock.

// NewsShown is how many stories the stock page shows: the newest the news
// filter kept.
const NewsShown = 2

const (
	newsPageMaxBytes  = 512 << 10 // og:image is in the head, near the top
	newsImageMaxBytes = 5 << 20
	newsImageMaxPix   = 40_000_000 // refuses images too big to decode safely
	newsThumbMaxSide  = 320        // shown at 96 × 64, so sharp on 3x screens
	newsThumbTimeout  = 12 * time.Second
	newsThumbAgent    = "Mozilla/5.0 (compatible; InvestorProjectBot/1.0; +https://investorproject.au)"
)

// fetchNewsThumb gets a story's thumbnail from its article's address. It's a
// var so tests can replace it.
var fetchNewsThumb = func(ctx context.Context, pageURL string) (contentType string, data []byte, err error) {
	return newsThumb(ctx, newsThumbClient(false), pageURL)
}

// newsThumbs fetches a picture for each of the stock's NewsShown newest shown
// stories that hasn't been tried, and drops pictures of stories that are no
// longer shown. A story whose picture can't be had is recorded with no data,
// so it's never tried again. Errors are logged, never returned: a missing
// picture mustn't stop the news run.
func newsThumbs(ctx context.Context, q *db.Queries, stockID int64, symbol string) {
	need, err := q.ListNewsNeedingThumb(ctx, db.ListNewsNeedingThumbParams{StockID: stockID, Shown: NewsShown})
	if err != nil {
		slog.Error("news thumbs list", "symbol", symbol, "error", err)
		return
	}
	for _, n := range need {
		ct, data, err := fetchNewsThumb(ctx, n.Url)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Debug("news thumb", "symbol", symbol, "url", n.Url, "error", err)
			ct, data = "", []byte{}
		}
		if err := q.SaveNewsThumb(ctx, db.SaveNewsThumbParams{NewsID: n.ID, ContentType: ct, Data: data}); err != nil {
			slog.Error("news thumb save", "symbol", symbol, "error", err)
		}
	}
	if err := q.PruneNewsThumbs(ctx, db.PruneNewsThumbsParams{StockID: stockID, Shown: NewsShown}); err != nil {
		slog.Error("news thumbs prune", "symbol", symbol, "error", err)
	}
}

// newsThumb reads the article at pageURL, finds its preview picture and
// returns it scaled to fit newsThumbMaxSide as a JPEG.
func newsThumb(ctx context.Context, client *http.Client, pageURL string) (string, []byte, error) {
	page, final, ct, err := newsGet(ctx, client, pageURL, "text/html,application/xhtml+xml", newsPageMaxBytes)
	if err != nil {
		return "", nil, err
	}
	if mt, _, _ := mime.ParseMediaType(ct); mt != "text/html" && mt != "application/xhtml+xml" {
		return "", nil, fmt.Errorf("page is %s, not HTML", ct)
	}
	src := pageImage(page, final)
	if src == "" {
		return "", nil, errors.New("no og:image")
	}
	img, _, _, err := newsGet(ctx, client, src, imageAccept, newsImageMaxBytes)
	if err != nil {
		return "", nil, fmt.Errorf("image: %w", err)
	}
	return thumbJPEG(img)
}

// imageAccept names only formats Go can decode. Image servers such as
// fool.com's and qz.com's (Cloudflare's format=auto) send AVIF to anyone
// who offers to take it.
const imageAccept = "image/webp,image/jpeg,image/png,image/gif;q=0.9"

// newsGet fetches u, reading at most limit bytes, and returns the body, the
// address it ended at after redirects, and its Content-Type.
func newsGet(ctx context.Context, client *http.Client, u, accept string, limit int64) ([]byte, *url.URL, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, "", err
	}
	req.Header.Set("User-Agent", newsThumbAgent)
	req.Header.Set("Accept", accept)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, "", fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, nil, "", fmt.Errorf("%d bytes, over %d", resp.ContentLength, limit)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, "", err
	}
	if int64(len(body)) > limit {
		// A page's head is near the top, so a long page is fine cut short;
		// an image cut short isn't.
		if strings.HasPrefix(resp.Header.Get("Content-Type"), "image/") {
			return nil, nil, "", fmt.Errorf("over %d bytes", limit)
		}
		body = body[:limit]
	}
	return body, resp.Request.URL, resp.Header.Get("Content-Type"), nil
}

// pageImage finds a page's preview picture: og:image, else twitter:image,
// made absolute against base. It reads only the head.
func pageImage(page []byte, base *url.URL) string {
	var og, tw string
	z := html.NewTokenizer(bytes.NewReader(page))
	for og == "" {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		name, hasAttr := z.TagName()
		tag := string(name)
		if tt == html.EndTagToken && tag == "head" || tt == html.StartTagToken && tag == "body" {
			break
		}
		if tag != "meta" || !hasAttr || (tt != html.StartTagToken && tt != html.SelfClosingTagToken) {
			continue
		}
		var key, content string
		for {
			k, v, more := z.TagAttr()
			switch string(k) {
			case "property", "name":
				if key == "" || string(k) == "property" {
					key = strings.ToLower(strings.TrimSpace(string(v)))
				}
			case "content":
				content = strings.TrimSpace(string(v))
			}
			if !more {
				break
			}
		}
		switch {
		case content == "":
		case key == "og:image" || key == "og:image:url" || key == "og:image:secure_url":
			og = content
		case (key == "twitter:image" || key == "twitter:image:src") && tw == "":
			tw = content
		}
	}
	src := og
	if src == "" {
		src = tw
	}
	if src == "" {
		return ""
	}
	u, err := url.Parse(src)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

// thumbJPEG decodes an image and returns it scaled to fit newsThumbMaxSide,
// re-encoded as JPEG. Transparent parts go white, the page's background.
// It's the same scaling as courseCoverImage in the handlers.
func thumbJPEG(data []byte) (string, []byte, error) {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("not an image we can read: %w", err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width*cfg.Height > newsImageMaxPix {
		return "", nil, fmt.Errorf("%s is %d × %d pixels", format, cfg.Width, cfg.Height)
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", nil, err
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w > newsThumbMaxSide || h > newsThumbMaxSide {
		if w >= h {
			w, h = newsThumbMaxSide, max(1, h*newsThumbMaxSide/w)
		} else {
			w, h = max(1, w*newsThumbMaxSide/h), newsThumbMaxSide
		}
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 80}); err != nil {
		return "", nil, err
	}
	return "image/jpeg", buf.Bytes(), nil
}

// newsThumbClient is the HTTP client for article pages and their pictures.
// Their addresses come from other people's pages, so it connects only to
// public addresses on ports 80 and 443, checked on every connection,
// redirects included: a page can't point it at Caddy's admin API on
// 127.0.0.1:2019, the app itself, or anything else on the box or its
// network. The check is on the resolved address the dialer is about to
// connect to, so a hostname that resolves to 127.0.0.1, or changes its
// answer between lookups (DNS rebinding), is caught too. It uses no proxy,
// so the check sees the real address. Tests pass allowLocal to reach an
// httptest server.
func newsThumbClient(allowLocal bool) *http.Client {
	if allowLocal {
		return newsThumbClientChecking(nil)
	}
	return newsThumbClientChecking(publicAddr)
}

// newsThumbClientChecking is newsThumbClient with check, if not nil, run on
// every connection's "ip:port". Tests use it to stand in for publicAddr.
func newsThumbClientChecking(check func(address string) error) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	if check != nil {
		dialer.Control = func(network, address string, _ syscall.RawConn) error {
			return check(address)
		}
	}
	return &http.Client{
		Timeout: newsThumbTimeout,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: 8 * time.Second,
			MaxIdleConns:          4,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to %s", req.URL.Scheme)
			}
			return nil
		},
	}
}

// notPublic are ranges that IsPrivate and the other netip tests leave out:
// shared address space (carrier-grade NAT), the NAT64 prefixes, which a
// NAT64 gateway turns into any IPv4 address including private ones, and the
// old IPv6 site-local range.
var notPublic = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("fec0::/10"),
}

// publicAddr refuses a connection to anything but a public address on port
// 80 or 443. address is the resolved "ip:port" the dialer is about to use.
func publicAddr(address string) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("refused %s: %w", address, err)
	}
	if p := ap.Port(); p != 80 && p != 443 {
		return fmt.Errorf("refused %s: port %d", address, p)
	}
	ip := ap.Addr().Unmap()
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return fmt.Errorf("refused %s: not a public address", address)
	}
	for _, p := range notPublic {
		if p.Contains(ip) {
			return fmt.Errorf("refused %s: not a public address", address)
		}
	}
	return nil
}
