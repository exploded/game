package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/exploded/game/internal/auth"
	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// Email sign-in. POST /login emails a link and a 6-digit code. The link
// opens a confirm page (mail scanners fetch links, so a GET mustn't sign in
// or use up the link); the code is typed into /login/check in the browser
// that asked for it, which the login_req cookie identifies. Signing in with
// an unknown address creates the account.
const (
	loginTTL         = 15 * time.Minute
	maxCodeAttempts  = 5
	maxLoginsPerHour = 5
	loginCookie      = "login_req"
	nextCookie       = "login_next" // the page to return to after signing in
)

// loginEmail is the data for the login email template.
type loginEmail struct {
	Token   string
	Code    string
	Minutes int
}

// LoginPage renders the sign-in form. ?next= is remembered in a cookie, so
// signing in (by link or code, in this browser) returns to that page.
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	if next := localPath(r.URL.Query().Get("next"), ""); next != "" {
		http.SetCookie(w, &http.Cookie{
			Name: nextCookie, Value: url.QueryEscape(next), Path: "/",
			MaxAge: int(loginTTL.Seconds()) * 2, HttpOnly: true, Secure: SecurityHeadersProd, SameSite: http.SameSiteLaxMode,
		})
	}
	h.render(w, r, "auth/login", "", PageData{Title: "Sign in"})
}

// takeNext returns the page saved by LoginPage, or fallback, and forgets it.
func takeNext(w http.ResponseWriter, r *http.Request, fallback string) string {
	c, err := r.Cookie(nextCookie)
	if err != nil {
		return fallback
	}
	http.SetCookie(w, &http.Cookie{Name: nextCookie, Value: "", Path: "/", MaxAge: -1})
	next, _ := url.QueryUnescape(c.Value)
	return localPath(next, fallback)
}

// LoginSubmit emails a sign-in link and code to the address entered.
func (h *Handler) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	entered := strings.TrimSpace(r.FormValue("email"))
	email, ok := mail.NormalizeAddress(entered)
	if !ok {
		h.loginError(w, r, entered, "Enter a valid email address, like name@example.com.")
		return
	}
	n, err := h.q.CountRecentLoginRequests(r.Context(), email)
	if err != nil {
		h.serverError(w, "count login requests", err)
		return
	}
	if n >= maxLoginsPerHour {
		h.loginError(w, r, entered, "We've sent several sign-in emails to that address in the last hour. Use the newest one, or try again later.")
		return
	}

	id, err := auth.RandomHex(16)
	if err != nil {
		h.serverError(w, "login id", err)
		return
	}
	token, err := auth.RandomHex(32)
	if err != nil {
		h.serverError(w, "login token", err)
		return
	}
	code, err := loginCode()
	if err != nil {
		h.serverError(w, "login code", err)
		return
	}
	if err := h.q.CreateLoginRequest(r.Context(), db.CreateLoginRequestParams{
		ID:        id,
		Email:     email,
		TokenHash: sha256Hex(token),
		CodeHash:  codeHash(id, code),
		ExpiresAt: time.Now().Add(loginTTL).UTC().Format(time.DateTime),
	}); err != nil {
		h.serverError(w, "create login request", err)
		return
	}
	if err := h.mailer.ToAddress(r.Context(), email, "login", loginEmail{
		Token: token, Code: code, Minutes: int(loginTTL / time.Minute),
	}); err != nil {
		h.serverError(w, "queue login email", err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     loginCookie,
		Value:    id,
		Path:     "/login",
		MaxAge:   int(loginTTL / time.Second),
		HttpOnly: true,
		Secure:   auth.IsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, "/login/check", http.StatusSeeOther)
}

func (h *Handler) loginError(w http.ResponseWriter, r *http.Request, email, msg string) {
	h.render(w, r, "auth/login", "", PageData{
		Title:  "Sign in",
		Errors: map[string]string{"email": msg},
		Extra:  map[string]any{"Email": email},
	})
}

// pendingLogin returns the sign-in request this browser started, if it's
// still usable.
func (h *Handler) pendingLogin(r *http.Request) (db.LoginRequest, bool) {
	c, err := r.Cookie(loginCookie)
	if err != nil || c.Value == "" {
		return db.LoginRequest{}, false
	}
	req, err := h.q.GetLoginRequest(r.Context(), c.Value)
	return req, err == nil
}

// LoginCheck asks for the code from the email.
func (h *Handler) LoginCheck(w http.ResponseWriter, r *http.Request) {
	req, ok := h.pendingLogin(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.render(w, r, "auth/check", "", PageData{
		Title: "Check your email",
		Extra: map[string]any{"Email": req.Email},
	})
}

// LoginCode signs in with the 6-digit code.
func (h *Handler) LoginCode(w http.ResponseWriter, r *http.Request) {
	req, ok := h.pendingLogin(r)
	if !ok {
		setFlashCookie(w, "That sign-in code has expired. Enter your email to get a new one.", "error")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	code := strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, r.FormValue("code"))
	if subtle.ConstantTimeCompare([]byte(codeHash(req.ID, code)), []byte(req.CodeHash)) != 1 {
		attempts, err := h.q.AddLoginAttempt(r.Context(), req.ID)
		if err != nil || attempts >= maxCodeAttempts {
			_, _ = h.q.UseLoginRequest(r.Context(), req.ID)
			setFlashCookie(w, "Too many wrong codes. Enter your email to get a new one.", "error")
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		h.render(w, r, "auth/check", "", PageData{
			Title:  "Check your email",
			Errors: map[string]string{"code": "That code isn't right. Check the newest email from StockGame."},
			Extra:  map[string]any{"Email": req.Email},
		})
		return
	}
	h.finishLogin(w, r, req)
}

// LoginVerifyPage is where the emailed link lands. It shows a button rather
// than signing in, so a mail scanner opening the link can't use it up.
func (h *Handler) LoginVerifyPage(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("t")
	req, err := h.q.GetLoginRequestByToken(r.Context(), sha256Hex(token))
	h.render(w, r, "auth/verify", "", PageData{
		Title: "Sign in",
		Extra: map[string]any{"Token": token, "Email": req.Email, "Expired": err != nil},
	})
}

// LoginVerify signs in with the emailed link's token.
func (h *Handler) LoginVerify(w http.ResponseWriter, r *http.Request) {
	req, err := h.q.GetLoginRequestByToken(r.Context(), sha256Hex(r.FormValue("t")))
	if err != nil {
		setFlashCookie(w, "That sign-in link has expired or was already used. Enter your email to get a new one.", "error")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	h.finishLogin(w, r, req)
}

// finishLogin uses up the request, finds or creates the user, and starts a
// session.
func (h *Handler) finishLogin(w http.ResponseWriter, r *http.Request, req db.LoginRequest) {
	ctx := r.Context()
	if n, err := h.q.UseLoginRequest(ctx, req.ID); err != nil || n == 0 {
		setFlashCookie(w, "That sign-in link has expired or was already used. Enter your email to get a new one.", "error")
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: loginCookie, Value: "", Path: "/login", MaxAge: -1})

	user, err := h.q.GetUserByEmail(ctx, req.Email)
	isNew := errors.Is(err, sql.ErrNoRows)
	if isNew {
		token, terr := auth.RandomHex(16)
		if terr != nil {
			h.serverError(w, "email token", terr)
			return
		}
		user, err = h.q.CreateUser(ctx, db.CreateUserParams{
			Email:      req.Email,
			Name:       nameFromEmail(req.Email),
			EmailToken: token,
		})
	}
	if err != nil {
		h.serverError(w, "find or create user", err)
		return
	}

	if user.IsAdmin == 0 && isAdminEmail(user.Email) {
		_ = h.q.SetAdmin(ctx, db.SetAdminParams{IsAdmin: 1, ID: user.ID})
	}
	if err := auth.StartSession(w, r, h.q, user.ID); err != nil {
		h.serverError(w, "start session", err)
		return
	}
	if isNew {
		if _, err := h.mailer.ToUser(ctx, user.ID, mail.Account, "welcome", nil); err != nil {
			slog.Error("queue welcome email", "user", user.ID, "error", err)
		}
		http.Redirect(w, r, "/welcome", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, takeNext(w, r, "/dashboard"), http.StatusSeeOther)
}

// WelcomePage asks a new player for a display name and whether they want
// news from Rob.
func (h *Handler) WelcomePage(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	h.render(w, r, "auth/welcome", "", PageData{
		Title: "Welcome",
		Extra: map[string]any{"Name": user.Name},
	})
}

// WelcomeSubmit saves the welcome form.
func (h *Handler) WelcomeSubmit(w http.ResponseWriter, r *http.Request) {
	user := auth.UserFromContext(r.Context())
	name, msg := cleanDisplayName(r.FormValue("name"))
	if msg != "" {
		h.render(w, r, "auth/welcome", "", PageData{
			Title:  "Welcome",
			Errors: map[string]string{"name": msg},
			Extra:  map[string]any{"Name": r.FormValue("name"), "News": r.FormValue("news") != ""},
		})
		return
	}
	if err := h.q.UpdateUserName(r.Context(), db.UpdateUserNameParams{Name: name, ID: user.ID}); err != nil {
		h.serverError(w, "update name", err)
		return
	}
	// Recorded either way: an explicit no is part of the consent record.
	news := int64(0)
	if r.FormValue("news") != "" {
		news = 1
	}
	if err := h.q.SetEmailPref(r.Context(), db.SetEmailPrefParams{
		UserID: user.ID, Category: string(mail.News), Enabled: news,
	}); err != nil {
		h.serverError(w, "set news pref", err)
		return
	}
	if next := takeNext(w, r, ""); next != "" {
		// Signed up from Learn: go back there.
		setFlashCookie(w, "You're all set.", "success")
		http.Redirect(w, r, next, http.StatusSeeOther)
		return
	}
	setFlashCookie(w, "You're all set. Join a game to get your $1,000,000.", "success")
	http.Redirect(w, r, "/games", http.StatusSeeOther)
}

// loginCode is a uniformly random 6-digit code.
func loginCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// codeHash ties a code to its request, so the same code in another request
// hashes differently.
func codeHash(id, code string) string {
	return sha256Hex(id + ":" + code)
}

// nameFromEmail is a starting display name: the part before the @.
func nameFromEmail(email string) string {
	local, _, _ := strings.Cut(email, "@")
	if name, msg := cleanDisplayName(local); msg == "" {
		return name
	}
	return "Player"
}

// cleanDisplayName trims and checks a display name. It returns the name, or
// a message saying what's wrong.
func cleanDisplayName(s string) (string, string) {
	name := strings.Join(strings.Fields(s), " ")
	switch {
	case name == "":
		return "", "Enter a name."
	case utf8.RuneCountInString(name) > 40:
		return "", "Use 40 characters or fewer."
	case strings.IndexFunc(name, unicode.IsControl) >= 0:
		return "", "Use letters, numbers and spaces."
	}
	return name, ""
}

// isAdminEmail reports whether email is in the comma-separated ADMIN_EMAIL.
func isAdminEmail(email string) bool {
	for _, ae := range strings.Split(os.Getenv("ADMIN_EMAIL"), ",") {
		if ae = strings.TrimSpace(ae); ae != "" && strings.EqualFold(ae, email) {
			return true
		}
	}
	return false
}

// serverError logs err and sends a plain 500.
func (h *Handler) serverError(w http.ResponseWriter, what string, err error) {
	slog.Error(what, "error", err)
	http.Error(w, "Something went wrong. Please try again.", http.StatusInternalServerError)
}
