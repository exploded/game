package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/exploded/game/internal/auth"
)

// SettingsPage renders the account settings page.
func (h *Handler) SettingsPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	h.render(w, r, "settings/index", "", PageData{
		Title: "Account Settings",
		Item:  user,
	})
}

// AccountDelete soft-deletes the user's account and logs them out.
func (h *Handler) AccountDelete(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

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

	data := map[string]any{
		"user": map[string]any{
			"id":          user.ID,
			"name":        user.Name,
			"email":       user.Email,
			"picture_url": user.PictureUrl,
			"created_at":  user.CreatedAt,
			"last_login":  user.LastLogin,
		},
		"achievements":   achievements,
		"participations": participations,
		"chat_messages":  chatMessages,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="stockgame-data-%d.json"`, user.ID))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}
