package handler

import (
	"database/sql"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
)

// PrivacyPage renders the privacy policy.
func (h *Handler) PrivacyPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "legal/privacy", "", PageData{Title: "Privacy Policy"})
}

// TermsPage renders the terms of service.
func (h *Handler) TermsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "legal/terms", "", PageData{Title: "Terms of Service"})
}

// AboutPage renders the about / how-to-play page.
func (h *Handler) AboutPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "legal/about", "", PageData{Title: "About"})
}

// HelpPage renders the FAQ / help page.
func (h *Handler) HelpPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "legal/help", "", PageData{Title: "Help & FAQ"})
}

// ContactPage renders the contact form.
func (h *Handler) ContactPage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	extra := map[string]any{"Token": h.contactToken(time.Now())}
	if user != nil {
		extra["PrefillName"] = user.Name
		extra["PrefillEmail"] = user.Email
	}
	h.render(w, r, "legal/contact", "", PageData{
		Title: "Contact Us",
		Extra: extra,
	})
}

// ContactSubmit handles the contact form POST.
func (h *Handler) ContactSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	email := strings.TrimSpace(r.FormValue("email"))
	message := strings.TrimSpace(r.FormValue("message"))

	errs := map[string]string{}
	if name == "" {
		errs["name"] = "Name is required"
	}
	if email == "" || !strings.Contains(email, "@") {
		errs["email"] = "A valid email is required"
	}
	if message == "" {
		errs["message"] = "Message is required"
	}
	if len(message) > 5000 {
		errs["message"] = "Message must be under 5000 characters"
	}

	user := auth.UserFromContext(r.Context())
	now := time.Now()
	var spam string // why a signed-out sender's message is dropped
	if user == nil {
		age, ok := h.contactTokenAge(r.FormValue("t"), now)
		switch {
		case r.FormValue("website") != "":
			spam = "hidden field filled"
		case !ok:
			spam = "no form token"
		case age < contactMinAge:
			spam = "sent too fast"
		case age > contactMaxAge && len(errs) == 0:
			errs["message"] = errContactExpired
		case looksLikeSpam(message, r.Host):
			spam = "sales pitch"
		}
	}
	if spam != "" {
		slog.Info("contact message dropped", "reason", spam, "name", name, "email", email,
			"message", truncate(message, 200))
		setFlashCookie(w, "Your message has been sent. We'll get back to you soon!", "success")
		http.Redirect(w, r, "/contact", http.StatusSeeOther)
		return
	}

	if len(errs) > 0 {
		h.render(w, r, "legal/contact", "", PageData{
			Title:  "Contact Us",
			Errors: errs,
			Extra: map[string]any{
				"PrefillName":  name,
				"PrefillEmail": email,
				"Message":      message,
				"Token":        h.contactToken(now),
			},
		})
		return
	}

	var userID sql.NullInt64
	if user != nil {
		userID = sql.NullInt64{Int64: user.ID, Valid: true}
	}

	_ = h.q.CreateContactMessage(r.Context(), db.CreateContactMessageParams{
		UserID:  userID,
		Name:    name,
		Email:   email,
		Message: message,
	})

	setFlashCookie(w, "Your message has been sent. We'll get back to you soon!", "success")
	http.Redirect(w, r, "/contact", http.StatusSeeOther)
}
