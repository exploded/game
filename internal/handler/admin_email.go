package handler

import (
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// AdminEmail is where an admin writes a newsletter to everyone who opted in
// to News from Rob.
func (h *Handler) AdminEmail(w http.ResponseWriter, r *http.Request) {
	h.renderAdminEmail(w, r, "", "", nil, nil)
}

func (h *Handler) renderAdminEmail(w http.ResponseWriter, r *http.Request, subject, body string, errs map[string]string, flash *Flash) {
	subs, err := h.q.ListNewsSubscribers(r.Context())
	if err != nil {
		h.serverError(w, "list news subscribers", err)
		return
	}
	sent, err := h.q.ListBroadcasts(r.Context())
	if err != nil {
		h.serverError(w, "list broadcasts", err)
		return
	}
	h.render(w, r, "admin/email", "", PageData{
		Title:  "Email",
		Flash:  flash,
		Items:  sent,
		Errors: errs,
		Extra: map[string]any{
			"Subscribers": len(subs),
			"Ready":       h.mailer.NewsReady(),
			"Subject":     subject,
			"Body":        body,
		},
	})
}

// AdminEmailSend sends the newsletter: as a test to the admin, or to every
// subscriber when the form's action is "send".
func (h *Handler) AdminEmailSend(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	subject := strings.TrimSpace(r.FormValue("subject"))
	body := strings.TrimSpace(r.FormValue("body"))
	errs := map[string]string{}
	if subject == "" || len(subject) > 150 {
		errs["subject"] = "Enter a subject of up to 150 characters."
	}
	if body == "" {
		errs["body"] = "Write the email."
	}
	if !h.mailer.NewsReady() {
		errs["form"] = "Set NEWS_SENDER in .env first. The Spam Act needs every marketing email to say who sent it."
	}
	if len(errs) > 0 {
		h.renderAdminEmail(w, r, subject, body, errs, nil)
		return
	}
	letter := mail.NewNewsletter(subject, body)

	if r.FormValue("action") != "send" {
		if err := h.mailer.Test(r.Context(), user.ID, mail.News, "news", letter); err != nil {
			h.serverError(w, "queue test newsletter", err)
			return
		}
		// Rendered rather than redirected, so the form keeps what was typed.
		h.renderAdminEmail(w, r, subject, body, nil, &Flash{
			Type: "success", Message: "Test sent to " + user.Email + ". Check it, then send it to everyone.",
		})
		return
	}

	subs, err := h.q.ListNewsSubscribers(r.Context())
	if err != nil {
		h.serverError(w, "list news subscribers", err)
		return
	}
	queued := 0
	for _, id := range subs {
		ok, err := h.mailer.ToUser(r.Context(), id, mail.News, "news", letter)
		if errors.Is(err, mail.ErrNoNewsSender) {
			break
		}
		if err != nil {
			slog.Error("queue newsletter", "user", id, "error", err)
			continue
		}
		if ok {
			queued++
		}
	}
	if err := h.q.CreateBroadcast(r.Context(), db.CreateBroadcastParams{
		Subject: subject, Body: body,
		SentBy:     sql.NullInt64{Int64: user.ID, Valid: true},
		Recipients: int64(queued),
	}); err != nil {
		slog.Error("record broadcast", "error", err)
	}
	setFlashCookie(w, "Newsletter queued for "+strconv.Itoa(queued)+" subscribers.", "success")
	http.Redirect(w, r, "/admin/email", http.StatusSeeOther)
}
