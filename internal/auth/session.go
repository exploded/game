package auth

import (
	"crypto/rand"
	"crypto/sha256"
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

// SessionKey is what the sessions table stores for a session cookie: its
// SHA-256, in hex. A copy of the database (a backup, say) then holds nothing
// that can be put in a cookie to sign in as someone. The CSRF token is still
// derived from the cookie itself.
func SessionKey(cookie string) string {
	sum := sha256.Sum256([]byte(cookie))
	return hex.EncodeToString(sum[:])
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
		ID:        SessionKey(sessionID),
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
			_ = queries.DeleteSession(r.Context(), SessionKey(cookie.Value))
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
		http.Redirect(w, r, "/", http.StatusSeeOther)
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
