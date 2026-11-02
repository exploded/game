package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/exploded/game/internal/db"
)

// API keys and OAuth tokens let a player's own AI (Claude, ChatGPT, their
// own agents) use the API and MCP server as them. Only a SHA-256 of each is
// stored, so a leaked database doesn't leak working tokens.

// TokenPrefix starts every API key and OAuth token, so they're easy to
// spot, in a leak scanner too.
const TokenPrefix = "wl_"

const tokenKey contextKey = "api_token"

// NewToken returns a new random token and the hash to store for it.
func NewToken() (token, hash string, err error) {
	raw, err := RandomHex(32)
	if err != nil {
		return "", "", err
	}
	token = TokenPrefix + raw
	return token, HashToken(token), nil
}

// HashToken is what's stored for a token or code.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// RequireToken lets a request through with a valid API key or OAuth access
// token in its Authorization header, putting the user and the token in the
// context. Cookies are never looked at, so these routes need no CSRF check.
// Without a valid token it answers 401 with the WWW-Authenticate header
// that sends an MCP client to metadataURL to sign in.
func RequireToken(queries *db.Queries, metadataURL func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			raw = strings.TrimSpace(raw)
			if !ok || raw == "" {
				unauthorized(w, r, metadataURL, "", "Send an API key or access token: Authorization: Bearer wl_...")
				return
			}
			tok, err := queries.GetAPITokenByHash(r.Context(), HashToken(raw))
			if err != nil || tok.Kind == "refresh" || tok.RevokedAt.Valid || expired(tok.ExpiresAt.String) {
				unauthorized(w, r, metadataURL, "invalid_token", "That API key or access token isn't valid. It may have expired or been revoked.")
				return
			}
			user, err := queries.GetUser(r.Context(), tok.UserID)
			if err != nil || user.BlockedAt.Valid || user.DeletedAt.Valid {
				unauthorized(w, r, metadataURL, "invalid_token", "This account can't use the API.")
				return
			}
			_ = queries.TouchAPIToken(r.Context(), tok.ID)
			touchLastSeen(r.Context(), queries, &user)
			ctx := context.WithValue(r.Context(), userKey, &user)
			ctx = context.WithValue(ctx, tokenKey, &tok)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// expired reports whether a token's expires_at (UTC, SQLite's datetime
// format) has passed. A key has none.
func expired(at string) bool {
	if at == "" {
		return false
	}
	t, err := time.Parse(time.DateTime, at)
	return err != nil || time.Now().After(t)
}

func unauthorized(w http.ResponseWriter, r *http.Request, metadataURL func(*http.Request) string, code, msg string) {
	challenge := `Bearer resource_metadata="` + metadataURL(r) + `"`
	if code != "" {
		challenge += `, error="` + code + `"`
	}
	w.Header().Set("WWW-Authenticate", challenge)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// TokenFromContext is the API key or access token RequireToken let in.
func TokenFromContext(ctx context.Context) *db.ApiToken {
	tok, _ := ctx.Value(tokenKey).(*db.ApiToken)
	return tok
}

// WithToken returns ctx carrying user and tok, as RequireToken does.
// Handler tests use it.
func WithToken(ctx context.Context, user *db.User, tok *db.ApiToken) context.Context {
	return context.WithValue(context.WithValue(ctx, userKey, user), tokenKey, tok)
}

// CanTrade reports whether the request's token may place and cancel orders.
func CanTrade(ctx context.Context) bool {
	tok := TokenFromContext(ctx)
	return tok != nil && tok.Scope == "trade"
}
