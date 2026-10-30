package auth

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/exploded/game/internal/db"
)

type contextKey string

const userKey contextKey = "user"

// RequireAuth checks for a valid session and injects the user into context.
// Without one it sends a page request to sign in, coming back afterwards.
func RequireAuth(queries *db.Queries, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			http.Redirect(w, r, loginURL(r), http.StatusTemporaryRedirect)
			return
		}

		session, err := queries.GetSession(r.Context(), cookie.Value)
		if err != nil {
			http.SetCookie(w, &http.Cookie{
				Name: "session", Value: "", Path: "/", MaxAge: -1,
			})
			http.Redirect(w, r, loginURL(r), http.StatusTemporaryRedirect)
			return
		}

		user, err := queries.GetUser(r.Context(), session.UserID)
		if err != nil {
			http.Redirect(w, r, loginURL(r), http.StatusTemporaryRedirect)
			return
		}
		touchLastSeen(r.Context(), queries, &user)

		ctx := context.WithValue(r.Context(), userKey, &user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// loginURL is the sign-in page, with next= set to come back to a GET
// request's page. The dashboard is the default, so it isn't added.
func loginURL(r *http.Request) string {
	if r.Method != http.MethodGet || r.URL.Path == "/" || r.URL.Path == "/dashboard" {
		return "/login"
	}
	return "/login?next=" + url.QueryEscape(r.URL.RequestURI())
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

// OptionalAuth sets the user in context if a valid session exists, but does not
// redirect when there is no session. Use for public pages that show extra info
// when the visitor happens to be logged in.
func OptionalAuth(queries *db.Queries, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		session, err := queries.GetSession(r.Context(), cookie.Value)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		user, err := queries.GetUser(r.Context(), session.UserID)
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		touchLastSeen(r.Context(), queries, &user)
		ctx := context.WithValue(r.Context(), userKey, &user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// lastSeenEvery is how stale users.last_seen gets before a request updates
// it, so browsing doesn't write to the database on every page.
const lastSeenEvery = time.Hour

// touchLastSeen records that user made a signed-in request, if last_seen is
// missing or older than lastSeenEvery.
func touchLastSeen(ctx context.Context, queries *db.Queries, user *db.User) {
	if user.LastSeen.Valid {
		seen, err := time.Parse(time.DateTime, user.LastSeen.String)
		if err == nil && time.Since(seen) < lastSeenEvery {
			return
		}
	}
	_ = queries.TouchLastSeen(ctx, user.ID)
}

// UserFromContext extracts the authenticated user from context.
func UserFromContext(ctx context.Context) *db.User {
	user, _ := ctx.Value(userKey).(*db.User)
	return user
}

// WithUser returns ctx carrying user, as RequireAuth does. Handler tests use
// it to skip the session lookup.
func WithUser(ctx context.Context, user *db.User) context.Context {
	return context.WithValue(ctx, userKey, user)
}
