package handler

import (
	"net/http"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// emailPref is one row of the Settings email form.
type emailPref struct {
	mail.CategoryInfo
	On bool
}

// SettingsEmail saves the email checkboxes on the Settings page.
func (h *Handler) SettingsEmail(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	for _, c := range mail.Optional {
		on := int64(0)
		if r.Form.Get(string(c.Key)) != "" {
			on = 1
		}
		if err := h.q.SetEmailPref(r.Context(), db.SetEmailPrefParams{
			UserID: user.ID, Category: string(c.Key), Enabled: on,
		}); err != nil {
			h.serverError(w, "set email pref", err)
			return
		}
	}
	setFlashCookie(w, "Email settings saved.", "success")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// SettingsProfile saves the display name.
func (h *Handler) SettingsProfile(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	name, msg := cleanDisplayName(r.FormValue("name"))
	if msg != "" {
		setFlashCookie(w, msg, "error")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}
	if err := h.q.UpdateUserName(r.Context(), db.UpdateUserNameParams{Name: name, ID: user.ID}); err != nil {
		h.serverError(w, "update name", err)
		return
	}
	setFlashCookie(w, "Name saved.", "success")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// UnsubscribePage is where an email's unsubscribe link lands. It asks for a
// click, so a mail scanner opening the link doesn't unsubscribe anyone.
func (h *Handler) UnsubscribePage(w http.ResponseWriter, r *http.Request) {
	user, info, ok := h.unsubscribeTarget(r)
	h.render(w, r, "email/unsubscribe", "", PageData{
		Title: "Unsubscribe",
		Extra: map[string]any{
			"Valid": ok, "Email": user.Email, "Category": info,
			"Action": r.URL.RequestURI(),
		},
	})
}

// Unsubscribe turns the category off. It serves the page's button and the
// RFC 8058 one-click POST that mail clients send.
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	user, info, ok := h.unsubscribeTarget(r)
	if ok {
		if err := h.q.SetEmailPref(r.Context(), db.SetEmailPrefParams{
			UserID: user.ID, Category: string(info.Key), Enabled: 0,
		}); err != nil {
			h.serverError(w, "unsubscribe", err)
			return
		}
	}
	h.render(w, r, "email/unsubscribe", "", PageData{
		Title: "Unsubscribe",
		Extra: map[string]any{"Valid": ok, "Done": ok, "Email": user.Email, "Category": info},
	})
}

// unsubscribeTarget reads the user and category from the link's t and c.
func (h *Handler) unsubscribeTarget(r *http.Request) (db.User, mail.CategoryInfo, bool) {
	info, ok := mail.Info(mail.Category(r.URL.Query().Get("c")))
	if !ok {
		return db.User{}, info, false
	}
	user, err := h.q.GetUserByEmailToken(r.Context(), r.URL.Query().Get("t"))
	if err != nil {
		return db.User{}, info, false
	}
	return user, info, true
}
