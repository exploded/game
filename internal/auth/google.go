package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	SessionDuration   = 30 * 24 * time.Hour
	SessionMaxAgeSecs = 30 * 24 * 60 * 60
	OAuthStateMaxAge  = 300
)

func IsSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

type GoogleUserInfo struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func OAuthConfig() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  os.Getenv("BASE_URL") + "/auth/google/callback",
		Scopes:       []string{"openid", "email", "profile"},
		Endpoint:     google.Endpoint,
	}
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := randomHex(16)
	if err != nil {
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   OAuthStateMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	url := OAuthConfig().AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func HandleCallback(queries *db.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stateCookie, err := r.Cookie("oauth_state")
		if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}

		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "No code provided", http.StatusBadRequest)
			return
		}

		oauthCtx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		token, err := OAuthConfig().Exchange(oauthCtx, code)
		if err != nil {
			http.Error(w, "Failed to exchange token", http.StatusInternalServerError)
			return
		}

		client := OAuthConfig().Client(oauthCtx, token)
		resp, err := client.Get("https://www.googleapis.com/oauth2/v2/userinfo")
		if err != nil {
			http.Error(w, "Failed to get user info", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			http.Error(w, "Google userinfo error", http.StatusInternalServerError)
			return
		}

		var info GoogleUserInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			http.Error(w, "Failed to decode user info", http.StatusInternalServerError)
			return
		}

		user, err := queries.UpsertUser(r.Context(), db.UpsertUserParams{
			GoogleID:   info.ID,
			Email:      info.Email,
			Name:       info.Name,
			PictureUrl: toNullString(info.Picture),
		})
		if err != nil {
			slog.Error("upsert user", "error", err)
			http.Error(w, "Failed to save user", http.StatusInternalServerError)
			return
		}

		// Auto-promote admin by email.
		adminEmails := os.Getenv("ADMIN_EMAIL")
		if adminEmails != "" && user.IsAdmin == 0 {
			for _, ae := range strings.Split(adminEmails, ",") {
				if strings.EqualFold(strings.TrimSpace(ae), info.Email) {
					_ = queries.SetAdmin(r.Context(), db.SetAdminParams{IsAdmin: 1, ID: user.ID})
					break
				}
			}
		}

		sessionID, err := randomHex(32)
		if err != nil {
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		expiresAt := time.Now().Add(SessionDuration).UTC().Format(time.DateTime)
		_, err = queries.CreateSession(r.Context(), db.CreateSessionParams{
			ID:        sessionID,
			UserID:    user.ID,
			ExpiresAt: expiresAt,
		})
		if err != nil {
			http.Error(w, "Failed to create session", http.StatusInternalServerError)
			return
		}

		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    sessionID,
			Path:     "/",
			MaxAge:   SessionMaxAgeSecs,
			HttpOnly: true,
			Secure:   IsSecure(r),
			SameSite: http.SameSiteLaxMode,
		})

		http.SetCookie(w, &http.Cookie{
			Name:     "oauth_state",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
		})

		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
	}
}

func HandleLogout(queries *db.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err == nil {
			_ = queries.DeleteSession(r.Context(), cookie.Value)
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   IsSecure(r),
			SameSite: http.SameSiteLaxMode,
		})
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("randomHex: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func toNullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
