package handler

import (
	"cmp"
	"log/slog"
	"net/http"
	"slices"
	"strconv"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	users, _ := h.q.ListUsers(r.Context())
	stockCount, _ := h.q.CountStocks(r.Context())

	h.render(w, r, "admin/index", "", PageData{
		Title: "Admin",
		Extra: map[string]any{
			"UserCount":  len(users),
			"StockCount": stockCount,
		},
	})
}

// userSorts are the orders /admin/users offers, by ?sort=. The query returns
// newest first, which is the default.
var userSorts = map[string]func(a, b db.ListUsersWithActivityRow) int{
	"seen":   func(a, b db.ListUsersWithActivityRow) int { return cmp.Compare(b.LastSeen.String, a.LastSeen.String) },
	"trades": func(a, b db.ListUsersWithActivityRow) int { return cmp.Compare(b.TradeCount, a.TradeCount) },
}

// AdminUsers lists every user with how much they play, and buttons to make
// them an admin or block them.
func (h *Handler) AdminUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.q.ListUsersWithActivity(r.Context())
	if err != nil {
		h.serverError(w, "list users", err)
		return
	}
	sort := r.URL.Query().Get("sort")
	if by, ok := userSorts[sort]; ok {
		slices.SortStableFunc(users, by)
	} else {
		sort = "joined"
	}
	blocked := 0
	for _, u := range users {
		if u.BlockedAt.Valid {
			blocked++
		}
	}
	h.render(w, r, "admin/users", "", PageData{
		Title: "Users",
		Items: users,
		Extra: map[string]any{"Sort": sort, "Blocked": blocked},
	})
}

// AdminBlockToggle blocks or unblocks a user. A blocked user is signed out
// everywhere, can't sign in again and gets no email.
func (h *Handler) AdminBlockToggle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user, err := h.q.GetUser(ctx, id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	back := "/admin/users"
	if s := r.FormValue("sort"); userSorts[s] != nil {
		back += "?sort=" + s
	}
	switch {
	case user.BlockedAt.Valid:
		if err := h.q.UnblockUser(ctx, id); err != nil {
			h.serverError(w, "unblock user", err)
			return
		}
		setFlashCookie(w, "Unblocked "+user.Name+". They can sign in again.", "success")
	case id == auth.UserFromContext(ctx).ID:
		setFlashCookie(w, "You can't block yourself", "error")
	case user.IsAdmin == 1:
		setFlashCookie(w, user.Name+" is an admin. Remove their admin first.", "error")
	default:
		if err := h.q.BlockUser(ctx, id); err != nil {
			h.serverError(w, "block user", err)
			return
		}
		if err := h.q.DeleteUserSessions(ctx, id); err != nil {
			h.serverError(w, "sign out blocked user", err)
			return
		}
		slog.Info("blocked user", "user", id, "by", auth.UserFromContext(ctx).ID)
		setFlashCookie(w, "Blocked "+user.Name+". They're signed out and can't sign in.", "success")
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) AdminToggle(w http.ResponseWriter, r *http.Request) {
	currentUser := auth.UserFromContext(r.Context())
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Prevent admins from demoting themselves.
	if id == currentUser.ID {
		setFlashCookie(w, "You cannot change your own admin status", "error")
		http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
		return
	}

	user, err := h.q.GetUser(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	newVal := int64(1)
	if user.IsAdmin == 1 {
		newVal = 0
	}
	_ = h.q.SetAdmin(r.Context(), db.SetAdminParams{IsAdmin: newVal, ID: id})
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (h *Handler) AdminContactMessages(w http.ResponseWriter, r *http.Request) {
	msgs, _ := h.q.ListContactMessages(r.Context(), db.ListContactMessagesParams{
		Limit: 100, Offset: 0,
	})
	h.render(w, r, "admin/messages", "", PageData{
		Title: "Contact Messages",
		Items: msgs,
	})
}

func (h *Handler) AdminMarkMessageRead(w http.ResponseWriter, r *http.Request) {
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = h.q.MarkContactMessageRead(r.Context(), mid)
	http.Redirect(w, r, "/admin/messages", http.StatusSeeOther)
}
