package handler

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/exploded/game"
)

func loadTestTemplates(t *testing.T) PageTemplates {
	t.Helper()
	static, err := NewStatic(game.Static())
	if err != nil {
		t.Fatal(err)
	}
	pages, err := LoadTemplates(game.Templates(), static)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	return pages
}

// TestLoadTemplatesFindsEveryPage checks the embedded FS holds every page,
// including partials, which a plain go:embed of a directory would skip.
func TestLoadTemplatesFindsEveryPage(t *testing.T) {
	pages := loadTestTemplates(t)
	want := 0
	fs.WalkDir(game.Templates(), "pages", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") && !strings.HasPrefix(d.Name(), "_") {
			want++
		}
		return err
	})
	if len(pages) != want || want == 0 {
		t.Errorf("loaded %d pages, want %d", len(pages), want)
	}
	if pages["games/list"].Lookup("nav") == nil {
		t.Error(`the "nav" partial isn't available to pages`)
	}
}

var (
	inlineHandler = regexp.MustCompile(`\son[a-z]+\s*=\s*["']`)
	scriptTag     = regexp.MustCompile(`<script\b[^>]*>`)
)

// TestTemplatesAreCSPSafe enforces what the CSP allows: inline scripts need
// the request nonce, inline event handlers never run, and scripts are
// self-hosted. Breaking any of these silently breaks the page in a browser.
func TestTemplatesAreCSPSafe(t *testing.T) {
	err := fs.WalkDir(game.Templates(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		b, err := fs.ReadFile(game.Templates(), p)
		if err != nil {
			return err
		}
		src := string(b)
		if m := inlineHandler.FindString(src); m != "" {
			t.Errorf("%s: inline event handler %q is blocked by the CSP; use a data- attribute and app.js", p, strings.TrimSpace(m))
		}
		for _, tag := range scriptTag.FindAllString(src, -1) {
			switch {
			case strings.Contains(tag, "src=\"http"):
				t.Errorf("%s: %s loads from another site; self-host it under static/js", p, tag)
			case !strings.Contains(tag, "src=") && !strings.Contains(tag, `nonce="{{.Nonce}}"`):
				t.Errorf("%s: inline %s needs nonce=\"{{.Nonce}}\"", p, tag)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestRenderedPage renders a real page through the middleware and checks the
// pieces the browser depends on.
func TestRenderedPage(t *testing.T) {
	h := New(nil, nil, loadTestTemplates(t), false, time.UTC)
	srv := SecurityHeaders(http.HandlerFunc(h.PrivacyPage))

	w := httptest.NewRecorder()
	srv.ServeHTTP(w, httptest.NewRequest("GET", "/privacy", nil))
	if w.Code != 200 {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
	body := w.Body.String()

	for _, want := range []string{
		`<script src="/static/js/htmx.min.js?v=`,
		`<link rel="stylesheet" href="/static/css/app.css?v=`,
		`<meta name="htmx-config"`,
		`hx-headers:inherited=`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q", want)
		}
	}
}
