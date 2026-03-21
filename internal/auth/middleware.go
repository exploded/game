package auth

import (
	"context"
	"net/http"

	"github.com/exploded/game/internal/db"
)

type contextKey string

const userKey contextKey = "user"

// RequireAuth checks for a valid session and injects the user into context.
func RequireAuth(queries *db.Queries, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		session, err := queries.GetSession(r.Context(), cookie.Value)
		if err != nil {
			http.SetCookie(w, &http.Cookie{
				Name: "session", Value: "", Path: "/", MaxAge: -1,
			})
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		user, err := queries.GetUser(r.Context(), session.UserID)
		if err != nil {
			http.Redirect(w, r, "/login", http.StatusTemporaryRedirect)
			return
		}

		ctx := context.WithValue(r.Context(), userKey, &user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAdmin checks that the current user is a site admin.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil || user.IsAdmin == 0 {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// UserFromContext extracts the authenticated user from context.
func UserFromContext(ctx context.Context) *db.User {
	user, _ := ctx.Value(userKey).(*db.User)
	return user
}
