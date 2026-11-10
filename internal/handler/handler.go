package handler

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
	"github.com/exploded/game/internal/media"
	"github.com/exploded/game/internal/moderate"
	"github.com/exploded/game/internal/vimeo"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	rawDB      *sql.DB
	q          *db.Queries
	pages      PageTemplates
	production bool
	loc        *time.Location
	mailer     *mail.Mailer
	media      *media.Service
	moderator  moderate.Checker // checks lobby messages; nil without ANTHROPIC_API_KEY
	vimeo      *vimeo.Client    // fills courses from Vimeo folders; nil without VIMEO_TOKEN
	screener   screenCache      // the stocks page's rows (screener.go)
	liveCache  liveBannerCache  // /live/banner's answer (livebanner.go)

	apiLimiter *RateLimiter // each player's API and MCP requests a minute; nil for no limit
}

func New(q *db.Queries, rawDB *sql.DB, pages PageTemplates, production bool, loc *time.Location) *Handler {
	displayLoc = loc
	return &Handler{
		rawDB:      rawDB,
		q:          q,
		pages:      pages,
		production: production,
		loc:        loc,
	}
}

// SetMailer gives the handlers a way to send email.
func (h *Handler) SetMailer(m *mail.Mailer) { h.mailer = m }

// SetMedia gives the handlers the video sync, for the admin pages.
func (h *Handler) SetMedia(m *media.Service) { h.media = m }

// SetModerator has lobby messages checked before they're posted.
func (h *Handler) SetModerator(m moderate.Checker) { h.moderator = m }

// SetVimeo lets admins fill a course from a Vimeo folder.
func (h *Handler) SetVimeo(c *vimeo.Client) { h.vimeo = c }

// PageData is the standard data envelope passed to all templates.
type PageData struct {
	Title     string
	CSRFToken string
	Nonce     string // CSP nonce; inline <script> tags need nonce="{{.Nonce}}"
	PageURL   string // this page's full URL without its query, for og:url; render sets it
	Flash     *Flash
	User      *db.User
	Items     any
	Item      any
	Extra     map[string]any
	Errors    map[string]string
	GameBar   *gameBar      // set by the GameContext middleware on /games/{id} pages
	LiveNow   *db.LiveEvent // a stream on now, for the "Rob is live" banner
	AdminNav  *adminNav     // the admin links, on /admin pages
	Nav       *siteNav      // the top bar's and tab bar's links, for signed-in users (sitenav.go)
}

// adminNav is the row of links at the top of every admin page
// (partials/_adminnav.html).
type adminNav struct {
	Tab     string // the section you're in ("home" on /admin)
	Unread  int64  // contact messages not yet read
	Blocked int64  // lobby messages moderation blocked, waiting for review
}

type Flash struct {
	Type    string // "success" | "error" | "info"
	Message string
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func setFlashCookie(w http.ResponseWriter, msg, kind string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "flash",
		Value:    kind + "|" + msg,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   30,
		SameSite: http.SameSiteLaxMode,
	})
}

func popFlashCookie(w http.ResponseWriter, r *http.Request) *Flash {
	c, err := r.Cookie("flash")
	if err != nil || c.Value == "" {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: "flash", Value: "", Path: "/", MaxAge: -1})
	kind, msg, ok := strings.Cut(c.Value, "|")
	if !ok {
		return nil
	}
	return &Flash{Type: kind, Message: msg}
}

// render renders pageName for full requests, or fragment for HTMX requests.
func (h *Handler) render(w http.ResponseWriter, r *http.Request, pageName, fragment string, data PageData) {
	tmpl, ok := h.pages[pageName]
	if !ok {
		slog.Error("template not found", "page", pageName)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	name := "base"
	if isHTMX(r) && fragment != "" {
		name = fragment
	}
	data.Nonce = cspNonce(r)
	if name == "base" {
		data.PageURL = siteURL(r) + r.URL.Path
		data.User = auth.UserFromContext(r.Context())
		if data.CSRFToken == "" {
			data.CSRFToken = auth.CSRFTokenFromContext(r.Context())
		}
		if data.Flash == nil {
			data.Flash = popFlashCookie(w, r)
		}
		if data.GameBar == nil {
			data.GameBar, _ = r.Context().Value(gameBarKey{}).(*gameBar)
		}
		if data.LiveNow == nil {
			data.LiveNow = h.liveNow(r)
		}
		if data.User != nil && data.Nav == nil {
			data.Nav = h.siteNav(r, data.User, data.GameBar)
		}
		if data.User != nil && data.User.IsAdmin == 1 {
			if tab := adminTab(r.URL.Path); tab != "" {
				unread, _ := h.q.CountUnreadContactMessages(r.Context())
				blocked, _ := h.q.CountBlockedLobbyMessages(r.Context())
				data.AdminNav = &adminNav{Tab: tab, Unread: unread, Blocked: blocked}
			}
		}
	}
	// Render into a buffer first: a template error halfway through would
	// otherwise have already sent a 200 and half a page.
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		slog.Error("template execute", "page", pageName, "fragment", name, "error", err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// adminTab is the admin section a path is in ("games" for /admin/games/3),
// "home" for /admin itself, or "" outside /admin.
func adminTab(path string) string {
	rest, ok := strings.CutPrefix(path, "/admin")
	if !ok || (rest != "" && rest[0] != '/') {
		return ""
	}
	tab, _, _ := strings.Cut(strings.TrimPrefix(rest, "/"), "/")
	if tab == "" {
		return "home"
	}
	return tab
}

func triggerToast(w http.ResponseWriter, msg, kind string) {
	payload, _ := json.Marshal(map[string]any{
		"showToast": map[string]string{"msg": msg, "type": kind},
	})
	w.Header().Set("HX-Trigger", string(payload))
}

func nint64(v int64) sql.NullInt64 {
	return sql.NullInt64{Int64: v, Valid: true}
}

// shares is "1 share" or "5 shares".
func shares(n int64) string {
	if n == 1 {
		return "1 share"
	}
	return fmt.Sprintf("%d shares", n)
}

// fmtCents formats cents as dollars: -2050 is "-$20.50", not "$-20.50", and
// -50 keeps its sign ("-$0.50").
func fmtCents(c int64) string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}
	return sign + "$" + fmtThousands(c/100) + fmt.Sprintf(".%02d", c%100)
}

// bigMoney formats whole dollars to the nearest trillion, billion or
// million: "$1.23B". Zero is "N/A", since stock_profiles stores a missing
// figure as 0.
func bigMoney(v int64) string {
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
}

// pctChange is c against total as a signed percentage to 2 places: "+1.25".
// A change that rounds to nothing is "0.00", never "-0.00" (a loss of $37
// on $1M).
func pctChange(c, total int64) string {
	if total == 0 {
		return "0.00"
	}
	pct := math.Round(float64(c-total)/float64(total)*10000) / 100
	if pct == 0 {
		return "0.00"
	}
	return fmt.Sprintf("%+.2f", pct)
}

// compactNumber shortens a count of shares: 48.2M, 15.6B, 920K.
func compactNumber(v int64) string {
	f := float64(v)
	switch {
	case v >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", f/1_000_000_000)
	case v >= 1_000_000:
		return fmt.Sprintf("%.1fM", f/1_000_000)
	case v >= 10_000:
		return fmt.Sprintf("%.0fK", f/1_000)
	}
	return fmtThousands(v)
}

func fmtThousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	neg := n < 0
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
