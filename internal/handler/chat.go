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

func (h *Handler) ChatPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	game, err := h.q.GetGame(r.Context(), gameID)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Must be a participant.
	_, err = h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		setFlashCookie(w, "Join the game first", "error")
		http.Redirect(w, r, fmt.Sprintf("/games/%d", gameID), http.StatusSeeOther)
		return
	}

	page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
	if page < 1 {
		page = 1
	}
	perPage := int64(50)
	offset := (page - 1) * perPage

	messages, _ := h.q.ListMessages(r.Context(), db.ListMessagesParams{
		GameID: gameID,
		Limit:  perPage,
		Offset: offset,
	})
	total, _ := h.q.CountMessages(r.Context(), gameID)

	h.render(w, r, "chat/index", "", PageData{
		Title: "Chat - " + game.Name,
		Item:  game,
		Items: messages,
		Extra: map[string]any{
			"Page":       page,
			"TotalPages": (total + perPage - 1) / perPage,
		},
	})
}

func (h *Handler) ChatSend(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Must be a participant.
	_, err = h.q.GetParticipantByGameAndUser(r.Context(), db.GetParticipantByGameAndUserParams{
		GameID: gameID, UserID: user.ID,
	})
	if err != nil {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	r.ParseForm()
	msg := strings.TrimSpace(r.FormValue("message"))
	if msg == "" {
		if isHTMX(r) {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/games/%d/chat", gameID), http.StatusSeeOther)
		return
	}

	// Limit message length.
	if len(msg) > 500 {
		msg = msg[:500]
	}

	_, _ = h.q.CreateMessage(r.Context(), db.CreateMessageParams{
		GameID:  gameID,
		UserID:  user.ID,
		Message: msg,
	})

	if isHTMX(r) {
		// Return new message HTML + clear the input.
		escaped := html.EscapeString(msg)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(fmt.Sprintf(`<div class="chat-message" id="msg-new" hx-swap-oob="afterbegin:#messages-list">
	<div class="chat-meta">
		<strong>%s</strong>
		<span class="text-muted">just now</span>
	</div>
	<p>%s</p>
</div>`, html.EscapeString(user.Name), escaped)))
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/games/%d/chat", gameID), http.StatusSeeOther)
}
