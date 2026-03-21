package handler

import "net/http"

// LoginPage renders the login page.
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "auth/login", "", PageData{
		Title: "Sign In",
	})
}
