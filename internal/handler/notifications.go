package handler

import (
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// notificationsPerPage is how many notifications a page of /notifications
// lists, newest first.
const notificationsPerPage = 25

func (h *Handler) NotificationsPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
	page = min(max(page, 1), maxPage)

	// One more than a page, to know whether there's an older page.
	notifications, _ := h.q.ListNotifications(r.Context(), db.ListNotificationsParams{
		UserID: user.ID,
		Limit:  notificationsPerPage + 1,
		Offset: (page - 1) * notificationsPerPage,
	})
	more := len(notifications) > notificationsPerPage
	if more {
		notifications = notifications[:notificationsPerPage]
	}

	h.render(w, r, "notifications/index", "", PageData{
		Title: "Notifications",
		Items: notifications,
		Extra: map[string]any{
			"Page": page,
			"More": more,
		},
	})
}

// NotificationMarkRead marks one notification read. Its Mark Read button
// (htmx) gets the notification back shown as read, with the bell's new count.
func (h *Handler) NotificationMarkRead(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("nid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if err := h.q.MarkNotificationRead(r.Context(), db.MarkNotificationReadParams{
		ID: id, UserID: user.ID,
	}); err != nil {
		h.serverError(w, "mark notification read", err)
		return
	}

	if !isHTMX(r) {
		http.Redirect(w, r, "/notifications", http.StatusSeeOther)
		return
	}
	n, err := h.q.GetNotification(r.Context(), db.GetNotificationParams{ID: id, UserID: user.ID})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	unread, _ := h.q.CountUnreadNotifications(r.Context(), user.ID)
	h.render(w, r, "notifications/index", "notification-read", PageData{
		Item:  n,
		Extra: map[string]any{"Unread": unread},
	})
}

// NotificationMarkAllRead marks every notification read. Its button is a
// plain form post.
func (h *Handler) NotificationMarkAllRead(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	if err := h.q.MarkAllNotificationsRead(r.Context(), user.ID); err != nil {
		h.serverError(w, "mark all notifications read", err)
		return
	}
	setFlashCookie(w, "All notifications marked as read", "success")
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}
