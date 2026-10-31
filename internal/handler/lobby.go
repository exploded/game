package handler

import (
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

func (h *Handler) LobbyPage(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
	if page < 1 {
		page = 1
	}
	perPage := int64(50)
	offset := (page - 1) * perPage

	// Players see posted messages. Admins see blocked ones too, or only
	// those with ?blocked=1 (the admin bar's Lobby link).
	user := auth.UserFromContext(r.Context())
	admin := user != nil && user.IsAdmin == 1
	blockedOnly := admin && r.URL.Query().Get("blocked") == "1"
	var showPosted, showBlocked, blocked int64 = 1, 0, 0
	if admin {
		showBlocked = 1
		blocked, _ = h.q.CountBlockedLobbyMessages(r.Context())
	}
	if blockedOnly {
		showPosted = 0
	}

	messages, _ := h.q.ListLobbyMessages(r.Context(), db.ListLobbyMessagesParams{
		ShowPosted:  showPosted,
		ShowBlocked: showBlocked,
		Limit:       perPage,
		Offset:      offset,
	})
	total, _ := h.q.CountLobbyMessages(r.Context(), db.CountLobbyMessagesParams{
		ShowPosted:  showPosted,
		ShowBlocked: showBlocked,
	})

	h.render(w, r, "lobby/index", "", PageData{
		Title: "Lobby",
		Items: messages,
		Extra: map[string]any{
			"Page":        page,
			"TotalPages":  (total + perPage - 1) / perPage,
			"BlockedOnly": blockedOnly,
			"Blocked":     blocked,
		},
	})
}

func (h *Handler) LobbySend(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	r.ParseForm()
	msg := strings.TrimSpace(r.FormValue("message"))
	if msg == "" {
		if isHTMX(r) {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, "/lobby", http.StatusSeeOther)
		return
	}

	if len(msg) > 500 {
		msg = msg[:500]
	}

	if h.moderator != nil {
		v, err := h.moderator.Check(r.Context(), msg)
		switch {
		case err != nil:
			// An outage shouldn't close the lobby, so the message goes up unchecked.
			slog.Warn("lobby moderation failed; posting unchecked", "user", user.ID, "error", err)
		case !v.Allow:
			// Kept for the admins to review, hidden from everyone else.
			if _, err := h.q.CreateLobbyMessage(r.Context(), db.CreateLobbyMessageParams{
				UserID:        user.ID,
				Message:       msg,
				BlockedReason: v.Reason,
			}); err != nil {
				slog.Error("save blocked lobby message", "user", user.ID, "error", err)
			}
			slog.Info("lobby message blocked", "user", user.ID, "reason", v.Reason)
			// Same answer as a refused trade: a toast, and the text left in
			// the box so the sender can change it.
			tradeError(w, r, "Not posted. "+v.Reason+" If that's a mistake, tell us through Contact.", "/lobby")
			return
		}
	}

	_, _ = h.q.CreateLobbyMessage(r.Context(), db.CreateLobbyMessageParams{
		UserID:  user.ID,
		Message: msg,
	})

	if isHTMX(r) {
		escaped := html.EscapeString(msg)
		avatarPath, _ := h.q.GetUserAvatarPath(r.Context(), user.ID)
		avatarHTML := avatarImg(user.Name, avatarPath)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf(`<div class="chat-message" hx-swap-oob="afterbegin target:#messages-list strip:false">
	<div class="chat-meta">
		%s
		<strong>%s</strong>
		<span class="text-muted">just now</span>
	</div>
	<p>%s</p>
</div>`, avatarHTML, html.EscapeString(user.Name), escaped)))
		return
	}
	http.Redirect(w, r, "/lobby", http.StatusSeeOther)
}

// AdminLobbyDelete removes a lobby message, as the terms allow.
func (h *Handler) AdminLobbyDelete(w http.ResponseWriter, r *http.Request) {
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_ = h.q.DeleteLobbyMessage(r.Context(), mid)
	http.Redirect(w, r, lobbyBack(r), http.StatusSeeOther)
}

// AdminLobbyUnblock posts a message moderation blocked by mistake. It shows
// where it was sent, not at the top.
func (h *Handler) AdminLobbyUnblock(w http.ResponseWriter, r *http.Request) {
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.q.UnblockLobbyMessage(r.Context(), mid); err != nil {
		h.serverError(w, "unblock lobby message", err)
		return
	}
	slog.Info("lobby message unblocked", "message", mid, "by", auth.UserFromContext(r.Context()).ID)
	setFlashCookie(w, "Posted. Everyone can see it now.", "success")
	http.Redirect(w, r, lobbyBack(r), http.StatusSeeOther)
}

// AdminLobbyBlockUser blocks the author of a lobby message, as /admin/users
// does. Their messages leave the lobby with them.
func (h *Handler) AdminLobbyBlockUser(w http.ResponseWriter, r *http.Request) {
	mid, err := strconv.ParseInt(r.PathValue("mid"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	m, err := h.q.GetLobbyMessage(r.Context(), mid)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	author, err := h.q.GetUser(r.Context(), m.UserID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.blockUser(w, r, author); err != nil {
		h.serverError(w, "block user", err)
		return
	}
	http.Redirect(w, r, lobbyBack(r), http.StatusSeeOther)
}

// lobbyBack keeps an admin working through blocked messages in that list.
func lobbyBack(r *http.Request) string {
	if r.FormValue("blocked") == "1" {
		return "/lobby?blocked=1"
	}
	return "/lobby"
}

// avatarImg returns HTML for a user's avatar image.
func avatarImg(name, avatarPath string) string {
	src := "/static/avatars/default.svg"
	if avatarPath != "" {
		src = avatarPath
	}
	return fmt.Sprintf(`<img src="%s" alt="%s" class="avatar-sm">`,
		html.EscapeString(src), html.EscapeString(name))
}
