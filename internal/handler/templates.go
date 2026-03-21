package handler

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

// PageTemplates maps page keys (e.g. "games/list") to cloned template sets.
type PageTemplates map[string]*template.Template

func LoadTemplates(dir string) (PageTemplates, error) {
	funcMap := template.FuncMap{
		"cents": func(v any) string {
			switch val := v.(type) {
			case int64:
				return fmtCents(val)
			case int:
				return fmtCents(int64(val))
			case float64:
				return fmtCents(int64(val))
			default:
				return "$0.00"
			}
		},
		"dollars": func(c int64) string {
			return "$" + fmtThousands(c/100)
		},
		"add":       func(a, b int) int { return a + b },
		"hasPrefix": strings.HasPrefix,
		"title": func(s string) string {
			if s == "" {
				return ""
			}
			return strings.ToUpper(s[:1]) + s[1:]
		},
		"dict": func(pairs ...any) map[string]any {
			m := make(map[string]any, len(pairs)/2)
			for i := 0; i+1 < len(pairs); i += 2 {
				if k, ok := pairs[i].(string); ok {
					m[k] = pairs[i+1]
				}
			}
			return m
		},
		"pct": func(c int64, total int64) string {
			if total == 0 {
				return "0.00"
			}
			pct := float64(c-total) / float64(total) * 100
			return fmt.Sprintf("%+.2f", pct)
		},
		"upper": strings.ToUpper,
		"rawDollars": func(c int64) string {
			return fmt.Sprintf("%.2f", float64(c)/100)
		},
		"slice": func(s string, start, end int) string {
			if start >= len(s) {
				return ""
			}
			if end > len(s) {
				end = len(s)
			}
			return s[start:end]
		},
		"subtract": func(a, b int64) int64 { return a - b },
		"int": func(v any) int {
			switch val := v.(type) {
			case int:
				return val
			case int64:
				return int(val)
			case float64:
				return int(val)
			default:
				return 0
			}
		},
	}

	// Build shared base from layouts + partials.
	base := template.New("").Funcs(funcMap)
	base = template.Must(base.ParseGlob(filepath.Join(dir, "layouts", "*.html")))

	// Parse partials if they exist.
	partials, _ := filepath.Glob(filepath.Join(dir, "partials", "*.html"))
	if len(partials) > 0 {
		base = template.Must(base.ParseFiles(partials...))
	}

	// Walk pages directory - clone base per page file.
	pages := make(PageTemplates)
	pagesDir := filepath.Join(dir, "pages")
	err := filepath.WalkDir(pagesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".html" {
			return err
		}
		// Skip fragment files (prefixed with _) as standalone pages.
		if strings.HasPrefix(d.Name(), "_") {
			return nil
		}
		rel, _ := filepath.Rel(pagesDir, path)
		key := filepath.ToSlash(rel[:len(rel)-len(".html")])

		clone := template.Must(base.Clone())
		pages[key] = template.Must(clone.ParseFiles(path))

		// Also parse sibling fragments (_*.html) into this clone.
		frags, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "_*.html"))
		for _, f := range frags {
			pages[key] = template.Must(pages[key].ParseFiles(f))
		}
		return nil
	})
	return pages, err
}
