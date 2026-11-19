package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
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
	// Avatars, with the achievement each locked one needs.
	avatars, _ := h.q.ListAvatars(r.Context())
	achieved, _ := h.q.ListUserAchievements(r.Context(), user.ID)
	earned := make(map[string]bool, len(achieved))
	for _, ua := range achieved {
		earned[ua.Key] = true
	}
	h.render(w, r, "settings/index", "", PageData{
		Title: "Settings",
		Item:  user,
		Items: rows,
		Extra: map[string]any{"Avatars": avatars, "EarnedKeys": earned},
	})
}

// AccountDelete deletes the user's account and signs them out everywhere.
// Requires the user to type "DELETE" to confirm.
//
// The users row stays, renamed "Deleted User" with a made-up address, so
// trades, scores, lobby messages and questions (which name their author
// through the users join) still make sense. Everything else that holds the
// address or is only the user's goes: sign-in requests, their email (sent or
// waiting), invites other players sent to the address (anonymised, so they
// still count as those players' invite), their watchlist, notifications,
// stream reminders and sessions. Contact messages stay until an admin deletes
// them, as the privacy policy says.
func (h *Handler) AccountDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	r.ParseForm()
	if r.FormValue("confirm_delete") != "DELETE" {
		setFlashCookie(w, "Please type DELETE to confirm account deletion.", "error")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	// Queued before the address is anonymised.
	if err := h.mailer.ToAddress(ctx, user.Email, "account_deleted", nil); err != nil {
		slog.Error("queue account deleted email", "user", user.ID, "error", err)
	}

	if err := h.deleteAccount(ctx, user); err != nil {
		slog.Error("delete account", "user", user.ID, "error", err)
		setFlashCookie(w, "Something went wrong. Please try again.", "error")
		http.Redirect(w, r, "/settings", http.StatusSeeOther)
		return
	}

	// Clear session cookie.
	http.SetCookie(w, &http.Cookie{
		Name: "session", Value: "", Path: "/", MaxAge: -1,
	})

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// deletedEmail is the address SoftDeleteUser gives a deleted user.
func deletedEmail(userID int64) string {
	return fmt.Sprintf("deleted-%d@deleted.local", userID)
}

// deleteAccount does AccountDelete's deleting, in one transaction.
func (h *Handler) deleteAccount(ctx context.Context, user *db.User) error {
	tx, err := h.rawDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := h.q.WithTx(tx)
	for _, step := range []func() error{
		func() error { return q.DeleteLoginRequestsForEmail(ctx, user.Email) },
		func() error {
			return q.DeleteEmailsForUser(ctx, db.DeleteEmailsForUserParams{UserID: nint64(user.ID), Email: user.Email})
		},
		func() error {
			return q.AnonymiseInvitesTo(ctx, db.AnonymiseInvitesToParams{
				Anon:  sql.NullString{String: deletedEmail(user.ID), Valid: true},
				Email: sql.NullString{String: user.Email, Valid: true},
			})
		},
		func() error { return q.DeleteUserWatchlist(ctx, user.ID) },
		func() error { return q.DeleteUserNotifications(ctx, user.ID) },
		func() error { return q.DeleteUserLiveReminders(ctx, user.ID) },
		func() error { return q.DeleteUserSessions(ctx, user.ID) },
		func() error { return q.SoftDeleteUser(ctx, user.ID) },
	} {
		if err := step(); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ExportPersonalData returns the user's personal data as a JSON download:
// their account, games and trades, what they wrote (lobby, questions,
// contact messages), the friends they invited, and their settings and
// progress.
func (h *Handler) ExportPersonalData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := auth.UserFromContext(ctx)

	achievements, _ := h.q.ListUserAchievements(ctx, user.ID)
	participations, _ := h.q.ListUserParticipations(ctx, user.ID)
	emailPrefs, _ := h.q.ListEmailPrefs(ctx, user.ID)
	transactions, _ := h.q.ExportUserTransactions(ctx, user.ID)
	watchlist, _ := h.q.ListWatchlist(ctx, user.ID)
	lobby, _ := h.q.ExportUserLobbyMessages(ctx, user.ID)
	questions, _ := h.q.ExportUserQuestions(ctx, user.ID)
	contact, _ := h.q.ExportUserContactMessages(ctx, nint64(user.ID))
	invites, _ := h.q.ExportUserInvites(ctx, user.ID)
	lessons, _ := h.q.ExportUserLessonProgress(ctx, user.ID)
	notifications, _ := h.q.ExportUserNotifications(ctx, user.ID)

	data := map[string]any{
		"user": map[string]any{
			"id":               user.ID,
			"name":             user.Name,
			"email":            user.Email,
			"created_at":       user.CreatedAt,
			"last_login":       user.LastLogin,
			"last_seen":        user.LastSeen.String,
			"age_confirmed_at": user.AgeConfirmedAt.String,
		},
		"achievements":     achievements,
		"participations":   participations,
		"transactions":     transactions,
		"watchlist":        watchlist,
		"lobby_messages":   lobby,
		"questions":        questions,
		"contact_messages": contact,
		"invites_sent":     invites,
		"lesson_progress":  lessons,
		"notifications":    notifications,
		"email_prefs":      emailPrefs,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="investor-project-data-%d.json"`, user.ID))
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(data)
}
