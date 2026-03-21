package handler

import (
	"net/http"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) NotificationsPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())

	page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
	if page < 1 {
		page = 1
	}
	perPage := int64(25)
	offset := (page - 1) * perPage

	notifications, _ := h.q.ListNotifications(r.Context(), db.ListNotificationsParams{
		UserID: user.ID,
		Limit:  perPage,
		Offset: offset,
	})

	h.render(w, r, "notifications/index", "", PageData{
		Title: "Notifications",
		Items: notifications,
		Extra: map[string]any{
			"Page": page,
		},
	})
}

func (h *Handler) NotificationMarkRead(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("nid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	_ = h.q.MarkNotificationRead(r.Context(), db.MarkNotificationReadParams{
		ID: id, UserID: user.ID,
	})

	if isHTMX(r) {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}

func (h *Handler) NotificationMarkAllRead(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	_ = h.q.MarkAllNotificationsRead(r.Context(), user.ID)

	if isHTMX(r) {
		triggerToast(w, "All notifications marked as read", "success")
		w.Header().Set("HX-Redirect", "/notifications")
		w.WriteHeader(http.StatusOK)
		return
	}
	setFlashCookie(w, "All notifications marked as read", "success")
	http.Redirect(w, r, "/notifications", http.StatusSeeOther)
}

// NotificationCount returns the unread count as an HTMX fragment for the nav badge.
func (h *Handler) NotificationCount(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	count, _ := h.q.CountUnreadNotifications(r.Context(), user.ID)

	w.Header().Set("Content-Type", "text/html")
	if count > 0 {
		w.Write([]byte(strconv.FormatInt(count, 10)))
	}
}
