package handler

import (
	"fmt"
	"html"
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

	messages, _ := h.q.ListLobbyMessages(r.Context(), db.ListLobbyMessagesParams{
		Limit:  perPage,
		Offset: offset,
	})
	total, _ := h.q.CountLobbyMessages(r.Context())

	h.render(w, r, "lobby/index", "", PageData{
		Title: "Lobby",
		Items: messages,
		Extra: map[string]any{
			"Page":       page,
			"TotalPages": (total + perPage - 1) / perPage,
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

	_, _ = h.q.CreateLobbyMessage(r.Context(), db.CreateLobbyMessageParams{
		UserID:  user.ID,
		Message: msg,
	})

	if isHTMX(r) {
		escaped := html.EscapeString(msg)
		avatarPath, _ := h.q.GetUserAvatarPath(r.Context(), user.ID)
		avatarHTML := avatarImg(user.Name, user.PictureUrl.String, avatarPath)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf(`<div class="chat-message" id="msg-new" hx-swap-oob="afterbegin:#messages-list">
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

// avatarImg returns HTML for a user's avatar image.
func avatarImg(name, pictureURL, avatarPath string) string {
	src := "/static/avatars/default.svg"
	if avatarPath != "" {
		src = avatarPath
	} else if pictureURL != "" {
		src = pictureURL
	}
	return fmt.Sprintf(`<img src="%s" alt="%s" class="avatar-sm">`,
		html.EscapeString(src), html.EscapeString(name))
}
