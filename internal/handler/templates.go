package handler

import (
	"database/sql"
	"fmt"
	"html/template"
	"io/fs"
	"path"
	"strings"
	"time"

	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/market"
)

// PageTemplates maps page keys (e.g. "games/list") to cloned template sets.
type PageTemplates map[string]*template.Template

// avatarPathMap maps avatar IDs to their image paths. Populated at startup.
var avatarPathMap = make(map[int64]string)

// RegisterAvatarPath records an avatar ID -> image path mapping for templates.
func RegisterAvatarPath(id int64, path string) {
	avatarPathMap[id] = path
}

// LoadTemplates parses the templates in fsys (layouts/, partials/, pages/).
// static supplies the cache-busting URLs for the "asset" func.
func LoadTemplates(fsys fs.FS, static *Static) (PageTemplates, error) {
	funcMap := template.FuncMap{
		"asset": static.URL,
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
		"upper":       strings.ToUpper,
		"paragraphs":  mail.Paragraphs,
		"marketLabel": market.MarketLabel,
		"bps": func(bp int64) string {
			return fmt.Sprintf("%.2f%%", float64(bp)/100)
		},
		"bigMoney": func(v int64) string {
			if v == 0 {
				return "N/A"
			}
			abs := v
			if abs < 0 {
				abs = -abs
			}
			switch {
			case abs >= 1_000_000_000_000:
				return fmt.Sprintf("$%.2fT", float64(v)/1_000_000_000_000)
			case abs >= 1_000_000_000:
				return fmt.Sprintf("$%.2fB", float64(v)/1_000_000_000)
			case abs >= 1_000_000:
				return fmt.Sprintf("$%.2fM", float64(v)/1_000_000)
			default:
				return "$" + fmtThousands(v)
			}
		},
		"ratio100": func(v int64) string {
			if v == 0 {
				return "N/A"
			}
			return fmt.Sprintf("%.2f", float64(v)/100)
		},
		"pct100": func(v int64) string {
			if v == 0 {
				return "N/A"
			}
			return fmt.Sprintf("%.2f%%", float64(v)/100)
		},
		"commas": func(v int64) string {
			if v == 0 {
				return "N/A"
			}
			return fmtThousands(v)
		},
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
		"multiply": func(a, b int64) int64 { return a * b },
		"avatarURL": func(pictureURL sql.NullString, avatarID sql.NullInt64) string {
			// Avatar lookup is done via a simple static path convention.
			// The actual avatar_id -> image_path mapping is handled by the
			// avatarPathMap which is populated at template load time.
			if avatarID.Valid && avatarID.Int64 > 0 {
				if path, ok := avatarPathMap[avatarID.Int64]; ok {
					return path
				}
			}
			if pictureURL.Valid && pictureURL.String != "" {
				return pictureURL.String
			}
			return "/static/avatars/default.svg"
		},
		"unixDate": func(ts int64) string {
			if ts == 0 {
				return ""
			}
			return time.Unix(ts, 0).UTC().Format("2006-01-02")
		},
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
	base, err := template.New("").Funcs(funcMap).ParseFS(fsys, "layouts/*.html")
	if err != nil {
		return nil, err
	}

	// Parse partials if they exist.
	if partials, _ := fs.Glob(fsys, "partials/*.html"); len(partials) > 0 {
		if base, err = base.ParseFS(fsys, partials...); err != nil {
			return nil, err
		}
	}

	// Walk pages directory - clone base per page file.
	pages := make(PageTemplates)
	err = fs.WalkDir(fsys, "pages", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".html" {
			return err
		}
		// Skip fragment files (prefixed with _) as standalone pages.
		if strings.HasPrefix(d.Name(), "_") {
			return nil
		}
		key := strings.TrimSuffix(strings.TrimPrefix(p, "pages/"), ".html")

		clone, err := base.Clone()
		if err != nil {
			return err
		}
		// Parse the page plus its sibling fragments (_*.html) into this clone.
		files := []string{p}
		frags, _ := fs.Glob(fsys, path.Join(path.Dir(p), "_*.html"))
		files = append(files, frags...)
		if pages[key], err = clone.ParseFS(fsys, files...); err != nil {
			return fmt.Errorf("parsing %s: %w", p, err)
		}
		return nil
	})
	return pages, err
}
