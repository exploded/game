package handler

import (
	"mime"
	"net/http"
	"strings"

	"github.com/exploded/game/internal/auth"
)

// MaxBodySize limits the size of incoming request bodies to maxBytes, except
// file uploads to admin pages (a course's cover picture), which get
// auth.MaxUploadBytes. CSRFProtect sets that limit too, but it can only
// lower a limit set here, never raise it, so without the exception every
// cover over maxBytes failed. Uploads elsewhere keep maxBytes: the sign-in
// and unsubscribe forms read multipart bodies too, before anyone is signed
// in.
func MaxBodySize(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := maxBytes
			if isAdminUpload(r) {
				limit = auth.MaxUploadBytes
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

// isAdminUpload reports whether r is a multipart form posted to /admin/.
// Only admins get past RequireAdmin there, though CSRFProtect reads the body
// first, for any signed-in user.
func isAdminUpload(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/admin/") {
		return false
	}
	mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mt == "multipart/form-data"
}
