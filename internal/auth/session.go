package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/exploded/game/internal/db"
)

const (
	SessionDuration   = 30 * 24 * time.Hour
	SessionMaxAgeSecs = 30 * 24 * 60 * 60
)

func IsSecure(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

// StartSession signs the user in: it stores a new session and sets the
// session cookie.
func StartSession(w http.ResponseWriter, r *http.Request, queries *db.Queries, userID int64) error {
	sessionID, err := RandomHex(32)
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(SessionDuration).UTC().Format(time.DateTime)
	if _, err := queries.CreateSession(r.Context(), db.CreateSessionParams{
		ID:        sessionID,
		UserID:    userID,
		ExpiresAt: expiresAt,
	}); err != nil {
		return fmt.Errorf("create session: %w", err)
	}
	_ = queries.TouchLastLogin(r.Context(), userID)
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   SessionMaxAgeSecs,
		HttpOnly: true,
		Secure:   IsSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
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

// RandomHex returns n random bytes as hex.
func RandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("RandomHex: %w", err)
	}
	return hex.EncodeToString(b), nil
}
