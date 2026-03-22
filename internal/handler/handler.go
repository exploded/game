package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	rawDB      *sql.DB
	q          *db.Queries
	pages      PageTemplates
	production bool
	loc        *time.Location
}

func New(q *db.Queries, rawDB *sql.DB, pages PageTemplates, production bool, loc *time.Location) *Handler {
	return &Handler{
		rawDB:      rawDB,
		q:          q,
		pages:      pages,
		production: production,
		loc:        loc,
	}
}

// PageData is the standard data envelope passed to all templates.
type PageData struct {
	Title     string
	CSRFToken string
	Flash     *Flash
	User      *db.User
	Items     any
	Item      any
	Extra     map[string]any
	Errors    map[string]string
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
	if name == "base" {
		data.User = auth.UserFromContext(r.Context())
		if data.Flash == nil {
			data.Flash = popFlashCookie(w, r)
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
		slog.Error("template execute", "page", pageName, "fragment", name, "error", err)
	}
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

// fmtCents formats a cent amount as "$X,XXX.XX".
func fmtCents(c int64) string {
	d := c / 100
	frac := c % 100
	if frac < 0 {
		frac = -frac
	}
	return "$" + fmtThousands(d) + fmt.Sprintf(".%02d", frac)
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
