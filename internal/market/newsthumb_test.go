package market

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/exploded/game/internal/db"
)

func TestPageImage(t *testing.T) {
	base, _ := url.Parse("https://www.example.com/news/story")
	for name, c := range map[string]struct{ page, want string }{
		"og:image": {`<html><head><meta property="og:image" content="https://cdn.example.com/a.jpg"></head></html>`,
			"https://cdn.example.com/a.jpg"},
		"content first, self-closing": {`<head><meta content="https://cdn.example.com/b.jpg" property="og:image" /></head>`,
			"https://cdn.example.com/b.jpg"},
		"name instead of property": {`<head><meta name="og:image" content="https://cdn.example.com/c.jpg"></head>`,
			"https://cdn.example.com/c.jpg"},
		"entities": {`<head><meta property="og:image" content="https://cdn.example.com/d.jpg?w=1&amp;h=2"></head>`,
			"https://cdn.example.com/d.jpg?w=1&h=2"},
		"relative": {`<head><meta property="og:image" content="/img/e.jpg"></head>`,
			"https://www.example.com/img/e.jpg"},
		"protocol-relative": {`<head><meta property="og:image" content="//cdn.example.com/f.jpg"></head>`,
			"https://cdn.example.com/f.jpg"},
		"twitter fallback": {`<head><meta name="twitter:image" content="https://cdn.example.com/t.jpg"></head>`,
			"https://cdn.example.com/t.jpg"},
		"og beats twitter": {`<head><meta name="twitter:image" content="https://cdn.example.com/t.jpg"><meta property="og:image" content="https://cdn.example.com/o.jpg"></head>`,
			"https://cdn.example.com/o.jpg"},
		"secure_url": {`<head><meta property="og:image:secure_url" content="https://cdn.example.com/s.jpg"></head>`,
			"https://cdn.example.com/s.jpg"},
		"empty content":  {`<head><meta property="og:image" content=""></head>`, ""},
		"none":           {`<head><title>x</title></head><body><p>hi</p></body>`, ""},
		"only in body":   {`<head></head><body><meta property="og:image" content="https://cdn.example.com/x.jpg"></body>`, ""},
		"not http":       {`<head><meta property="og:image" content="javascript:alert(1)"></head>`, ""},
		"data URL":       {`<head><meta property="og:image" content="data:image/png;base64,AAAA"></head>`, ""},
		"other metadata": {`<head><meta property="og:title" content="Title"><meta name="description" content="d"></head>`, ""},
	} {
		if got := pageImage([]byte(c.page), base); got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func TestPublicAddr(t *testing.T) {
	for addr, ok := range map[string]bool{
		"93.184.216.34:443":                    true,
		"93.184.216.34:80":                     true,
		"[2606:2800:220:1::248]:443":           true,
		"93.184.216.34:8080":                   false, // only 80 and 443
		"127.0.0.1:2019":                       false, // Caddy's admin API
		"127.0.0.1:443":                        false,
		"10.0.0.5:443":                         false,
		"192.168.1.1:80":                       false,
		"172.16.0.1:443":                       false,
		"169.254.169.254:80":                   false, // cloud metadata
		"100.64.0.1:443":                       false, // shared address space
		"0.0.0.0:443":                          false,
		"[::1]:443":                            false,
		"[fe80::1]:443":                        false,
		"[fd00::1]:443":                        false,
		"[::ffff:127.0.0.1]:443":               false, // IPv4 loopback inside IPv6
		"[2400:8907::2000:55ff:fea2:cd9f]:443": true,
		"224.0.0.1:443":                        false,
		"[64:ff9b::7f00:1]:443":                false, // NAT64 for 127.0.0.1
		"[64:ff9b:1::a00:5]:443":               false, // local-use NAT64
		"[fec0::1]:443":                        false, // old site-local
		"not an address":                       false,
	} {
		if err := publicAddr(addr); (err == nil) != ok {
			t.Errorf("publicAddr(%q) = %v, want allowed %v", addr, err, ok)
		}
	}
}

// TestNewsThumbClientRefusesLocal: the real client won't connect to a
// server on this machine, which is where an httptest server is.
func TestNewsThumbClientRefusesLocal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the request reached the local server")
	}))
	defer srv.Close()
	if _, _, err := newsThumb(context.Background(), newsThumbClient(false), srv.URL); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("error %v, want a refusal", err)
	}
}

// TestNewsThumbRedirectsChecked: every connection is checked, so neither a
// redirect nor an og:image can lead from an allowed site to a refused one.
// "outside" stands in for a public news site and "inside" for something on
// the box, such as Caddy's admin API; check lets only outside through, as
// publicAddr would.
func TestNewsThumbRedirectsChecked(t *testing.T) {
	inside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("the request reached the refused server: %s", r.URL)
	}))
	defer inside.Close()
	outside := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, inside.URL+"/config/", http.StatusFound)
		case "/redirect-twice":
			http.Redirect(w, r, "/redirect", http.StatusFound)
		case "/picture-inside":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, `<head><meta property="og:image" content="%s/pic.png"></head>`, inside.URL)
		case "/picture-redirect":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<head><meta property="og:image" content="/redirect"></head>`)
		}
	}))
	defer outside.Close()
	allowed := strings.TrimPrefix(outside.URL, "http://")
	client := newsThumbClientChecking(func(address string) error {
		if address != allowed {
			return fmt.Errorf("refused %s", address)
		}
		return nil
	})
	for _, path := range []string{"/redirect", "/redirect-twice", "/picture-inside", "/picture-redirect"} {
		_, _, err := newsThumb(context.Background(), client, outside.URL+path)
		if err == nil || !strings.Contains(err.Error(), "refused") {
			t.Errorf("%s: error %v, want a refusal", path, err)
		}
	}
}

// pngOf is a w × h PNG.
func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, 0, color.NRGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestNewsThumb(t *testing.T) {
	big := pngOf(t, 1200, 600)
	mux := http.NewServeMux()
	mux.HandleFunc("/story", func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.UserAgent(), "InvestorProjectBot") {
			t.Errorf("User-Agent %q", r.UserAgent())
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><meta property="og:image" content="/pic.png"></head><body>story</body></html>`)
	})
	mux.HandleFunc("/moved", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/story", http.StatusMovedPermanently)
	})
	mux.HandleFunc("/pic.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write(big)
	})
	mux.HandleFunc("/bare", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><head><title>no picture</title></head></html>`)
	})
	mux.HandleFunc("/broken", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<head><meta property="og:image" content="/notimage"></head>`)
	})
	mux.HandleFunc("/notimage", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		fmt.Fprint(w, "<html>not really</html>")
	})
	mux.HandleFunc("/huge", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<head><meta property="og:image" content="/huge.jpg"></head>`)
	})
	mux.HandleFunc("/huge.jpg", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(make([]byte, newsImageMaxBytes+10))
	})
	mux.HandleFunc("/pdf", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		fmt.Fprint(w, "%PDF")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	client := newsThumbClient(true)
	ctx := context.Background()

	for _, path := range []string{"/story", "/moved"} {
		ct, data, err := newsThumb(ctx, client, srv.URL+path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		img, err := jpeg.Decode(bytes.NewReader(data))
		if ct != "image/jpeg" || err != nil {
			t.Fatalf("%s: %s, decode error %v", path, ct, err)
		}
		if b := img.Bounds(); b.Dx() != 320 || b.Dy() != 160 {
			t.Errorf("%s: thumbnail is %d × %d, want 320 × 160", path, b.Dx(), b.Dy())
		}
	}

	for path, want := range map[string]string{
		"/bare":    "no og:image",
		"/broken":  "not an image",
		"/huge":    "bytes",
		"/pdf":     "not HTML",
		"/missing": "http 404",
	} {
		if _, _, err := newsThumb(ctx, client, srv.URL+path); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: error %v, want one containing %q", path, err, want)
		}
	}
}

// TestNewsThumbs: only the newest NewsShown shown stories get a picture; a
// failure is recorded so it isn't tried again; pictures of stories that
// drop out of the newest go.
func TestNewsThumbs(t *testing.T) {
	d, q := monthlyDB(t)
	ctx := context.Background()
	for _, stmt := range []string{
		`INSERT INTO stocks (id, symbol, name, market, currency) VALUES (1, 'A', 'A', 'sp500', 'USD')`,
		`INSERT INTO stock_news (id, stock_id, headline, url, published_at, shown) VALUES
			(1, 1, 'old', 'https://x/old', 100, 1),
			(2, 1, 'fails', 'https://x/fails', 200, 1),
			(3, 1, 'hidden', 'https://x/hidden', 300, 0),
			(4, 1, 'newest', 'https://x/newest', 400, 1)`,
	} {
		if _, err := d.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	var asked []string
	old := fetchNewsThumb
	fetchNewsThumb = func(_ context.Context, u string) (string, []byte, error) {
		asked = append(asked, u)
		if strings.HasSuffix(u, "fails") {
			return "", nil, errors.New("blocked")
		}
		return "image/jpeg", []byte("jpeg:" + u), nil
	}
	defer func() { fetchNewsThumb = old }()

	thumbs := func() string {
		rows, err := d.Query(`SELECT news_id, length(data) > 0 FROM news_thumbs ORDER BY news_id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var id int64
			var has bool
			rows.Scan(&id, &has)
			out = append(out, fmt.Sprintf("%d:%v", id, has))
		}
		return strings.Join(out, " ")
	}

	newsThumbs(ctx, q, 1, "A")
	sort.Strings(asked)
	if got := fmt.Sprint(asked); got != "[https://x/fails https://x/newest]" {
		t.Errorf("asked for %s, want the newest two shown", got)
	}
	if got := thumbs(); got != "2:false 4:true" {
		t.Errorf("news_thumbs %q, want 2:false 4:true", got)
	}
	news, err := q.ListStockNews(ctx, db.ListStockNewsParams{StockID: 1, Limit: NewsShown})
	if err != nil {
		t.Fatal(err)
	}
	if len(news) != 2 || !news[0].HasThumb || news[1].HasThumb {
		t.Errorf("ListStockNews: %+v", news)
	}

	// Run again: nothing left to try.
	asked = nil
	newsThumbs(ctx, q, 1, "A")
	if len(asked) != 0 {
		t.Errorf("asked again for %v", asked)
	}

	// A newer story pushes "fails" out of the newest two, so its record goes.
	if _, err := d.Exec(`INSERT INTO stock_news (id, stock_id, headline, url, published_at, shown) VALUES (5, 1, 'newer', 'https://x/newer', 500, 1)`); err != nil {
		t.Fatal(err)
	}
	newsThumbs(ctx, q, 1, "A")
	if got := thumbs(); got != "4:true 5:true" {
		t.Errorf("news_thumbs %q after a newer story, want 4:true 5:true", got)
	}

	// A deleted story's picture goes too.
	d.Exec(`DELETE FROM stock_news WHERE id = 4`)
	newsThumbs(ctx, q, 1, "A")
	if got := thumbs(); strings.Contains(got, "4:") {
		t.Errorf("news_thumbs %q still has the deleted story", got)
	}
}
