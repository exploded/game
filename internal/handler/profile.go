package handler

import (
	"database/sql"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// ProfileUpdateAvatar sets the player's avatar, from Settings. Locked
// avatars need their achievement.
func (h *Handler) ProfileUpdateAvatar(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	r.ParseForm()

	avatarID, err := strconv.ParseInt(r.FormValue("avatar_id"), 10, 64)
	if err != nil {
		http.Error(w, "Invalid avatar", http.StatusBadRequest)
		return
	}

	// Validate avatar exists and user can use it.
	avatar, err := h.q.GetAvatar(r.Context(), avatarID)
	if err != nil {
		http.Error(w, "Avatar not found", http.StatusNotFound)
		return
	}

	if avatar.Category == "achievement" && avatar.AchievementKey.Valid {
		// Check user has the required achievement.
		ach, err := h.q.GetAchievementByKey(r.Context(), avatar.AchievementKey.String)
		if err != nil {
			http.Error(w, "Achievement not found", http.StatusBadRequest)
			return
		}
		has, _ := h.q.HasAchievement(r.Context(), db.HasAchievementParams{
			UserID:        user.ID,
			AchievementID: ach.ID,
		})
		if has == 0 {
			if isHTMX(r) {
				triggerToast(w, "You haven't unlocked this avatar yet!", "error")
				return
			}
			setFlashCookie(w, "You haven't unlocked this avatar yet!", "error")
			http.Redirect(w, r, "/settings", http.StatusSeeOther)
			return
		}
	}

	_ = h.q.UpdateUserAvatar(r.Context(), db.UpdateUserAvatarParams{
		AvatarID: sql.NullInt64{Int64: avatarID, Valid: true},
		ID:       user.ID,
	})

	if isHTMX(r) {
		triggerToast(w, "Avatar updated!", "success")
		w.Header().Set("HX-Refresh", "true")
		return
	}
	setFlashCookie(w, "Avatar updated!", "success")
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}
