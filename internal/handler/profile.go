package handler

import (
	"context"
	"database/sql"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// giveRandomAvatar sets a new player's avatar to one of the free ones, picked
// at random, so they don't start with a blank circle. A failure leaves them
// without one, as before, and they can still choose one in Settings.
func (h *Handler) giveRandomAvatar(ctx context.Context, userID int64) {
	avatars, err := h.q.ListAvatars(ctx)
	if err != nil {
		slog.Error("random avatar: list avatars", "user", userID, "error", err)
		return
	}
	var free []int64
	for _, a := range avatars {
		if a.Category == "free" {
			free = append(free, a.ID)
		}
	}
	if len(free) == 0 {
		return
	}
	if err := h.q.UpdateUserAvatar(ctx, db.UpdateUserAvatarParams{
		AvatarID: sql.NullInt64{Int64: free[rand.IntN(len(free))], Valid: true},
		ID:       userID,
	}); err != nil {
		slog.Error("random avatar: set avatar", "user", userID, "error", err)
	}
}

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
