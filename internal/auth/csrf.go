package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
)

const csrfTokenKey contextKey = "csrf_token"

// LoadCSRFSecret returns the CSRF secret stored in app_secrets, creating a
// random 32-byte one the first time.
func LoadCSRFSecret(sqlDB *sql.DB) ([]byte, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if _, err := sqlDB.Exec("INSERT OR IGNORE INTO app_secrets (name, value) VALUES ('csrf', ?)", secret); err != nil {
		return nil, fmt.Errorf("store csrf secret: %w", err)
	}
	if err := sqlDB.QueryRow("SELECT value FROM app_secrets WHERE name = 'csrf'").Scan(&secret); err != nil {
		return nil, fmt.Errorf("load csrf secret: %w", err)
	}
	return secret, nil
}

// csrfToken derives a CSRF token from the session ID using HMAC-SHA256.
func csrfToken(sessionID string, secret []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(sessionID))
	return hex.EncodeToString(mac.Sum(nil))
}

// CSRFProtect is middleware that generates and validates CSRF tokens.
// It derives tokens from the session cookie using HMAC, so no extra storage is needed.
func CSRFProtect(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Generate token from session if available.
			var token string
			if cookie, err := r.Cookie("session"); err == nil && cookie.Value != "" {
				token = csrfToken(cookie.Value, secret)
			}

			// Validate on state-changing methods.
			if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodDelete || r.Method == http.MethodPatch {
				if token == "" {
					http.Error(w, "Forbidden — no session", http.StatusForbidden)
					return
				}

				// Accept token from header (HTMX) or form field.
				submitted := r.Header.Get("X-CSRF-Token")
				if submitted == "" {
					if err := r.ParseForm(); err == nil {
						submitted = r.FormValue("_csrf")
					}
				}

				if !hmac.Equal([]byte(submitted), []byte(token)) {
					http.Error(w, "Forbidden — invalid CSRF token", http.StatusForbidden)
					return
				}
			}

			// Store token in context for templates.
			ctx := context.WithValue(r.Context(), csrfTokenKey, token)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CSRFTokenFromContext retrieves the CSRF token from context.
func CSRFTokenFromContext(ctx context.Context) string {
	if token, ok := ctx.Value(csrfTokenKey).(string); ok {
		return token
	}
	return ""
}
