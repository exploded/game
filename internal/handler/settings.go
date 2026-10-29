package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/mail"
)

// SettingsPage renders the account settings page.
func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	prefs, err := mail.Prefs(r.Context(), h.q, user.ID)
	if err != nil {
		h.serverError(w, "email prefs", err)
		return
	}
	rows := make([]emailPref, len(mail.Optional))
	for i, c := range mail.Optional {
		rows[i] = emailPref{CategoryInfo: c, On: prefs[c.Key]}
	}
	h.render(w, r, "settings/index", "", PageData{
		Title: "Account Settings",
		Item:  user,
		Items: rows,
	})
}

// AccountDelete soft-deletes the user's account and logs them out.
// Requires the user to type "DELETE" to confirm.
func (h *Handler) AccountDelete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	r.ParseForm()
	if r.FormValue("confirm_delete") != "DELETE" {
		setFlashCookie(w, "Please type DELETE to confirm account deletion.", "error")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	// Queued before the address is anonymised.
	if err := h.mailer.ToAddress(r.Context(), user.Email, "account_deleted", nil); err != nil {
		slog.Error("queue account deleted email", "user", user.ID, "error", err)
	}

	// Anonymise user record.
	if err := h.q.SoftDeleteUser(r.Context(), user.ID); err != nil {
		setFlashCookie(w, "Something went wrong. Please try again.", "error")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	// Destroy all sessions.
	_ = h.q.DeleteUserSessions(r.Context(), user.ID)

	// Clear session cookie.
	http.SetCookie(w, &http.Cookie{
		Name: "session", Value: "", Path: "/", MaxAge: -1,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ExportPersonalData returns all user data as a JSON download.
func (h *Handler) ExportPersonalData(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	achievements, _ := h.q.ListUserAchievements(r.Context(), user.ID)
	participations, _ := h.q.ListUserParticipations(r.Context(), user.ID)
	chatMessages, _ := h.q.ListUserChatMessages(r.Context(), user.ID)
	emailPrefs, _ := h.q.ListEmailPrefs(r.Context(), user.ID)

	data := map[string]any{
		"user": map[string]any{
			"id":         user.ID,
			"name":       user.Name,
			"email":      user.Email,
			"created_at": user.CreatedAt,
			"last_login": user.LastLogin,
		},
		"achievements":   achievements,
		"participations": participations,
		"chat_messages":  chatMessages,
		"email_prefs":    emailPrefs,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="stockgame-data-%d.json"`, user.ID))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}
