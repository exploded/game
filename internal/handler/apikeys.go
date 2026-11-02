package handler

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// maxAPIKeys is how many live API keys a player can have.
const maxAPIKeys = 10

// APIKeysPage is /settings/api: how to connect an AI, the player's API keys
// and the apps connected through OAuth.
func (h *Handler) APIKeysPage(w http.ResponseWriter, r *http.Request) {
	h.renderAPIKeys(w, r, "", "")
}

// renderAPIKeys draws /settings/api. newKey is a key just made, shown this
// once; problem is a reason the last form was refused.
func (h *Handler) renderAPIKeys(w http.ResponseWriter, r *http.Request, newKey, problem string) {
	user := auth.UserFromContext(r.Context())
	keys, err := h.q.ListAPIKeys(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, "api keys", err)
		return
	}
	apps, err := h.q.ListAppConnections(r.Context(), user.ID)
	if err != nil {
		h.serverError(w, "app connections", err)
		return
	}
	h.render(w, r, "settings/api", "", PageData{
		Title: "Connect your AI",
		Items: keys,
		Extra: map[string]any{
			"Apps":    apps,
			"NewKey":  newKey,
			"Problem": problem,
			"MCPURL":  siteURL(r) + "/mcp",
			"Site":    siteURL(r),
			"Full":    len(keys) >= maxAPIKeys,
		},
	})
}

// APIKeyCreate makes an API key and shows it once.
func (h *Handler) APIKeyCreate(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	r.ParseForm()
	name := strings.TrimSpace(r.FormValue("name"))
	scope := r.FormValue("access")
	switch {
	case name == "":
		h.renderAPIKeys(w, r, "", "Give the key a name, such as the AI that will use it.")
		return
	case utf8.RuneCountInString(name) > 40:
		h.renderAPIKeys(w, r, "", "Keep the name to 40 characters.")
		return
	case scope != "read" && scope != "trade":
		h.renderAPIKeys(w, r, "", "Choose what the key can do.")
		return
	}
	if keys, _ := h.q.ListAPIKeys(r.Context(), user.ID); len(keys) >= maxAPIKeys {
		h.renderAPIKeys(w, r, "", "You have 10 keys. Revoke one you don't use first.")
		return
	}
	token, hash, err := auth.NewToken()
	if err != nil {
		h.serverError(w, "new api key", err)
		return
	}
	if _, err := h.q.CreateAPIToken(r.Context(), db.CreateAPITokenParams{
		UserID:    user.ID,
		Kind:      "key",
		Name:      name,
		TokenHash: hash,
		Prefix:    token[:len(auth.TokenPrefix)+6],
		Scope:     scope,
	}); err != nil {
		h.serverError(w, "create api key", err)
		return
	}
	// Drawn straight away rather than redirected, so the key never sits in
	// a cookie or a URL.
	h.renderAPIKeys(w, r, token, "")
}

// APIKeyRevoke revokes one of the player's API keys.
func (h *Handler) APIKeyRevoke(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, _ := strconv.ParseInt(r.PathValue("kid"), 10, 64)
	if n, err := h.q.RevokeAPIKey(r.Context(), db.RevokeAPIKeyParams{ID: id, UserID: user.ID}); err != nil || n == 0 {
		setFlashCookie(w, "That key was revoked already", "error")
	} else {
		setFlashCookie(w, "Key revoked. Anything using it can't any more.", "success")
	}
	http.Redirect(w, r, "/settings/api", http.StatusSeeOther)
}

// AppRevoke disconnects an app connected through OAuth: its access and
// refresh tokens stop working.
func (h *Handler) AppRevoke(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	grant := r.PathValue("grant")
	if n, err := h.q.RevokeUserGrant(r.Context(), db.RevokeUserGrantParams{
		GrantID: sql.NullString{String: grant, Valid: true}, UserID: user.ID,
	}); err != nil || n == 0 {
		setFlashCookie(w, "That app was disconnected already", "error")
	} else {
		setFlashCookie(w, "Disconnected. The app can't see your portfolio or trade any more.", "success")
	}
	http.Redirect(w, r, "/settings/api", http.StatusSeeOther)
}
